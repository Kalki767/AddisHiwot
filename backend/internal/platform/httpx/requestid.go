// Package httpx holds HTTP plumbing shared by every feature: request IDs,
// access logging and running a server with a graceful shutdown.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

// RequestIDHeader carries the request ID in requests and responses.
const RequestIDHeader = "X-Request-ID"

type ctxKey struct{}

// A client-supplied ID is kept only if it is short and plain, so it cannot
// inject fake lines or control characters into the logs.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID gives every request an ID: the caller's X-Request-ID when it is
// valid, otherwise a new random one. The ID is echoed in the response header
// and stored in the request context for logs and error responses.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

// RequestIDFrom returns the request ID stored by RequestID, or "" if there is none.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error since Go 1.24
	return hex.EncodeToString(b)
}
