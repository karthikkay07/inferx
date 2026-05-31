package middleware

import (
	"net/http"
	"time"
)

// Timeout enforces a hard deadline on every request.
// Returns 503 with a JSON body if the handler does not complete in time.
// Uses http.TimeoutHandler which handles concurrent writes safely.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, `{"error":"request timeout"}`)
	}
}
