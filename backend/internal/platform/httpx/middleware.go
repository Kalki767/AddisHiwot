package httpx

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
)

// Recover turns a panic in a handler into a logged 500 in the error envelope,
// so one bug cannot crash the process or leave the client hanging.
func Recover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler { //nolint:errorlint // the sentinel is compared, not wrapped
					panic(v) // the server's own signal to drop the connection
				}
				logger.ErrorContext(r.Context(), "panic",
					slog.String("request_id", RequestIDFrom(r.Context())),
					slog.String("panic", fmt.Sprint(v)),
					slog.String("stack", string(debug.Stack())))
				writeEnvelope(w, r, &Error{Status: http.StatusInternalServerError, Code: CodeInternal,
					Message: "Something went wrong. Please try again."})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// MuxErrors replaces the plain-text 404 and 405 pages that http.ServeMux
// writes with the JSON error envelope, so clients only ever see one error format.
func MuxErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&muxErrorWriter{ResponseWriter: w, r: r}, r)
	})
}

type muxErrorWriter struct {
	http.ResponseWriter
	r       *http.Request
	swapped bool
}

func (m *muxErrorWriter) WriteHeader(status int) {
	plain := strings.HasPrefix(m.Header().Get("Content-Type"), "text/plain")
	if plain && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) {
		m.swapped = true
		m.Header().Del("X-Content-Type-Options")
		he := &Error{Status: http.StatusNotFound, Code: CodeNotFound, Message: "No such endpoint."}
		if status == http.StatusMethodNotAllowed {
			he = &Error{Status: status, Code: CodeMethodNotAllowed, Message: "This endpoint does not support " + m.r.Method + "."}
		}
		writeEnvelope(m.ResponseWriter, m.r, he)
		return
	}
	m.ResponseWriter.WriteHeader(status)
}

func (m *muxErrorWriter) Write(b []byte) (int, error) {
	if m.swapped {
		return len(b), nil // drop the plain-text body; the JSON one is already written
	}
	return m.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (m *muxErrorWriter) Unwrap() http.ResponseWriter { return m.ResponseWriter }
