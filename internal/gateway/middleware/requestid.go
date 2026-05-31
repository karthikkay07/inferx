package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type reqIDKey struct{}

const requestIDHeader = "X-Request-ID"

// RequestID propagates an existing X-Request-ID from the client or generates
// a new one. The ID is stored in context and echoed back in the response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		ctx := context.WithValue(r.Context(), reqIDKey{}, id)
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(reqIDKey{}).(string)
	return id
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
