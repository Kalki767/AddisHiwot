package httpx

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// echoID responds with the request ID it finds in the context.
var echoID = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(RequestIDFrom(r.Context())))
})

func TestRequestIDGeneratedWhenMissing(t *testing.T) {
	rec := httptest.NewRecorder()
	RequestID(echoID).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	id := rec.Header().Get(RequestIDHeader)
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) {
		t.Fatalf("generated ID %q is not 32 hex characters", id)
	}
	if rec.Body.String() != id {
		t.Errorf("context ID %q differs from header ID %q", rec.Body.String(), id)
	}
}

func TestRequestIDGeneratedIDsDiffer(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		rec := httptest.NewRecorder()
		RequestID(echoID).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		id := rec.Header().Get(RequestIDHeader)
		if seen[id] {
			t.Fatalf("ID %q generated twice", id)
		}
		seen[id] = true
	}
}

func TestRequestIDKeepsValidClientID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "app-7f3a.42_x")
	rec := httptest.NewRecorder()
	RequestID(echoID).ServeHTTP(rec, req)

	if got := rec.Header().Get(RequestIDHeader); got != "app-7f3a.42_x" {
		t.Errorf("header = %q, want the client's ID", got)
	}
	if rec.Body.String() != "app-7f3a.42_x" {
		t.Errorf("context = %q, want the client's ID", rec.Body.String())
	}
}

func TestRequestIDReplacesUnsafeClientID(t *testing.T) {
	for name, bad := range map[string]string{
		"newline (log injection)": "abc\n{\"level\":\"ERROR\"}",
		"spaces":                  "a b",
		"too long":                strings.Repeat("a", 65),
		"quote":                   `a"b`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(RequestIDHeader, bad)
			rec := httptest.NewRecorder()
			RequestID(echoID).ServeHTTP(rec, req)
			if got := rec.Header().Get(RequestIDHeader); got == bad || len(got) != 32 {
				t.Errorf("unsafe ID was not replaced: %q", got)
			}
		})
	}
}

func TestRequestIDFromEmptyContext(t *testing.T) {
	if got := RequestIDFrom(httptest.NewRequest(http.MethodGet, "/", nil).Context()); got != "" {
		t.Errorf("RequestIDFrom = %q, want empty", got)
	}
}
