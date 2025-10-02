package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/health"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/httpx"
)

type upDB struct{}

func (upDB) Ping(context.Context) error { return nil }

func newTestRouter(logs *bytes.Buffer) http.Handler {
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	return NewRouter(Deps{Logger: logger, Health: health.New(upDB{}, time.Second, logger)})
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/readyz", http.StatusOK},
		{http.MethodHead, "/healthz", http.StatusOK},
		{http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/readyz", http.StatusMethodNotAllowed},
		{http.MethodGet, "/nope", http.StatusNotFound},
	}
	h := newTestRouter(&bytes.Buffer{})
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
			if rec.Header().Get(httpx.RequestIDHeader) == "" {
				t.Error("response has no request ID, even on errors")
			}
			if tt.want >= 400 && (rec.Header().Get("Content-Type") != "application/json" ||
				!strings.Contains(rec.Body.String(), `"request_id"`)) {
				t.Errorf("error response is not the JSON envelope: %q", rec.Body.String())
			}
		})
	}
}

func TestHealthChecksAreNotAccessLogged(t *testing.T) {
	var logs bytes.Buffer
	h := newTestRouter(&logs)
	for _, path := range []string{"/healthz", "/readyz"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if logs.Len() != 0 {
		t.Errorf("health checks were logged:\n%s", logs.String())
	}

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope", nil))
	if !strings.Contains(logs.String(), `"path":"/nope"`) {
		t.Errorf("other requests must be access-logged, got:\n%s", logs.String())
	}
}
