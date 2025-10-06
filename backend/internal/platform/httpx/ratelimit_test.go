package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// limitAfter allows n requests per key, then refuses with wait.
type limitAfter struct {
	n    int
	wait time.Duration
	seen map[string]int
}

func (l *limitAfter) Allow(key string) (bool, time.Duration) {
	l.seen[key]++
	if l.seen[key] > l.n {
		return false, l.wait
	}
	return true, 0
}

func TestLimitByIPAnswers429WithRetryAfter(t *testing.T) {
	lim := &limitAfter{n: 2, wait: 1500 * time.Millisecond, seen: map[string]int{}}
	h := RequestID(LimitByIP(lim, quietLogger(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	call := func(remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
		req.RemoteAddr = remote
		req.Header.Set("X-Forwarded-For", "203.0.113.9") // must be ignored
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for range 2 {
		if rec := call("198.51.100.7:5555"); rec.Code != http.StatusNoContent {
			t.Fatalf("allowed request got %d", rec.Code)
		}
	}
	rec := call("198.51.100.7:6666") // same IP, other port
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request: %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Errorf("Retry-After = %q, want 2 (1.5s rounded up)", got)
	}
	var env envelopeJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Error.Code != CodeRateLimited {
		t.Errorf("body = %s", rec.Body)
	}
	if rec := call("198.51.100.8:5555"); rec.Code != http.StatusNoContent {
		t.Errorf("another IP was limited: %d", rec.Code)
	}
	if lim.seen["ip:203.0.113.9"] != 0 {
		t.Error("X-Forwarded-For was trusted")
	}
}

func TestRetryAfterIsAtLeastOneSecond(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteTooManyRequests(rec, httptest.NewRequest(http.MethodGet, "/", nil), quietLogger(), 10*time.Millisecond)
	if rec.Header().Get("Retry-After") != "1" {
		t.Errorf("Retry-After = %q, want 1", rec.Header().Get("Retry-After"))
	}
}

func TestClientIP(t *testing.T) {
	for remote, want := range map[string]string{"192.0.2.1:1234": "192.0.2.1", "[2001:db8::1]:80": "2001:db8::1", "odd": "odd"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remote
		if got := ClientIP(req); got != want {
			t.Errorf("ClientIP(%q) = %q, want %q", remote, got, want)
		}
	}
}
