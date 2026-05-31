package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const clientTTL = 5 * time.Minute

type Limiter struct {
	mu      sync.Mutex
	clients map[string]*clientState
	rate    rate.Limit
	burst   int
}

type clientState struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewLimiter(rps float64, burst int) *Limiter {
	l := &Limiter{
		clients: make(map[string]*clientState),
		rate:    rate.Limit(rps),
		burst:   burst,
	}
	go l.cleanup()
	return l
}

func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientKey(r)
		if !l.allow(key) {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *Limiter) allow(key string) bool {
	l.mu.Lock()
	c, ok := l.clients[key]
	if !ok {
		c = &clientState{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.clients[key] = c
	}
	c.lastSeen = time.Now()
	l.mu.Unlock()
	return c.limiter.Allow()
}

// cleanup removes client entries that haven't been seen recently.
func (l *Limiter) cleanup() {
	ticker := time.NewTicker(clientTTL)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		for k, c := range l.clients {
			if time.Since(c.lastSeen) > clientTTL {
				delete(l.clients, k)
			}
		}
		l.mu.Unlock()
	}
}

// clientKey uses API key when present (more generous budget), falls back to IP.
func clientKey(r *http.Request) string {
	if k := r.Header.Get("X-API-Key"); k != "" {
		return "key:" + k
	}
	return "ip:" + realIP(r)
}

func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.SplitN(ip, ",", 2)[0]
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if i := strings.LastIndex(r.RemoteAddr, ":"); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}
