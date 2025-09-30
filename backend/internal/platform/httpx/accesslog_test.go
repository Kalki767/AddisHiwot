package httpx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAccessLogWritesOneJSONLine(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := RequestID(AccessLog(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hello"))
	})))

	req := httptest.NewRequest(http.MethodPost, "/v1/check-ins?token=secret", nil)
	req.Header.Set(RequestIDHeader, "req-1")
	h.ServeHTTP(httptest.NewRecorder(), req)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log is not one JSON line: %v\n%s", err, buf.String())
	}
	want := map[string]any{"msg": "request", "request_id": "req-1", "method": "POST",
		"path": "/v1/check-ins", "status": float64(418), "bytes": float64(5)}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("%s = %v, want %v", k, line[k], v)
		}
	}
	if _, ok := line["duration_ms"]; !ok {
		t.Error("duration_ms missing")
	}
	if bytes.Contains(buf.Bytes(), []byte("secret")) {
		t.Error("query string leaked into the log")
	}
}

func TestAccessLogDefaultsStatusTo200(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := AccessLog(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line["status"] != float64(200) {
		t.Errorf("status = %v, want 200", line["status"])
	}
}
