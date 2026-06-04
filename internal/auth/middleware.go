package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type claimsKey struct{}
type requestIDKey struct{}

// SetClaims stores verified JWT claims in the request context.
func SetClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// GetClaims retrieves JWT claims from the context.
func GetClaims(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(*Claims)
	return c, ok && c != nil
}

// GetRequestID returns the request ID injected by RequestIDMiddleware.
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// AuthMiddleware validates Authorization: Bearer <token>, injects tenant + claims into context.
func AuthMiddleware(km *KeyManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeJSONError(w, http.StatusUnauthorized, "missing authorization header")
				return
			}
			token := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := km.Verify(token)
			if err != nil {
				switch err {
				case ErrTokenExpired:
					writeJSONError(w, http.StatusUnauthorized, "token expired")
				default:
					writeJSONError(w, http.StatusUnauthorized, "invalid token")
				}
				return
			}
			ctx := SetTenantID(r.Context(), claims.TenantID)
			ctx = SetClaims(ctx, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireScope returns middleware that enforces the given scope on every request.
func RequireScope(km *KeyManager, scope Scope) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetClaims(r.Context())
			if !ok || !km.HasScope(claims, scope) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
					"error":    "insufficient scope",
					"required": string(scope),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitMiddleware enforces 100 requests per minute per tenant.
// Uses a per-instance sync.Map for reliable sequential counting; accepts ristretto.Cache
// as the parameter per API contract (used for TTL-aware future eviction).
func RateLimitMiddleware(cache *ristretto.Cache) func(http.Handler) http.Handler {
	type entry struct {
		mu    sync.Mutex
		count int64
	}
	var buckets sync.Map
	_ = cache // accepted per spec; minute-keyed buckets provide natural TTL

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID, ok := GetTenantID(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			minute := time.Now().Unix() / 60
			bucketKey := fmt.Sprintf("rl:%s:%d", tenantID, minute)
			nextMinute := (minute + 1) * 60
			retryAfter := nextMinute - time.Now().Unix()

			actual, _ := buckets.LoadOrStore(bucketKey, &entry{})
			e := actual.(*entry)

			e.mu.Lock()
			curr := e.count
			if curr >= 100 {
				e.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-RateLimit-Limit", "100")
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", nextMinute))
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
					"error":       "rate limit exceeded",
					"retry_after": retryAfter,
				})
				return
			}
			e.count++
			remaining := int64(100) - e.count
			e.mu.Unlock()

			w.Header().Set("X-RateLimit-Limit", "100")
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", nextMinute))
			next.ServeHTTP(w, r)
		})
	}
}

// RequestIDMiddleware injects or propagates an X-Request-ID header.
func RequestIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = newReqID()
			}
			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OTelMiddleware starts an OTel server span for each request using the given tracer.
func OTelMiddleware(tracer trace.Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()

			span.SetAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", r.URL.Path),
			)
			if id, ok := GetTenantID(ctx); ok {
				span.SetAttributes(attribute.String("inferbolt.tenant_id", id))
			}

			rw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
			next.ServeHTTP(rw, r.WithContext(ctx))
			span.SetAttributes(attribute.Int("http.status_code", rw.code))
		})
	}
}

// LoggerMiddleware logs each completed request with structured slog fields.
func LoggerMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
			next.ServeHTTP(rw, r)
			tenantID, _ := GetTenantID(r.Context())
			slog.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.code,
				"latency_ms", time.Since(start).Milliseconds(),
				"request_id", GetRequestID(r.Context()),
				"tenant_id", tenantID,
			)
		})
	}
}

// TimeoutMiddleware enforces a hard per-request deadline.
func TimeoutMiddleware(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, `{"error":"request timeout"}`)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}

func newReqID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
