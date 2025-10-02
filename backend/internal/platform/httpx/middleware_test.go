package httpx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecoverTurnsPanicIntoA500(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := RequestID(Recover(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("nil map write in checkin handler")
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	var env envelopeJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("not JSON: %s", rec.Body.String())
	}
	if rec.Code != http.StatusInternalServerError || env.Error.Code != CodeInternal {
		t.Errorf("got %d %+v", rec.Code, env.Error)
	}
	if strings.Contains(rec.Body.String(), "nil map") {
		t.Error("panic value leaked to the client")
	}
	if !strings.Contains(logs.String(), "nil map write") || !strings.Contains(logs.String(), "stack") {
		t.Errorf("panic not logged with its stack: %s", logs.String())
	}
}

func TestRecoverLetsAbortHandlerThrough(t *testing.T) {
	h := Recover(quietLogger(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		if v := recover(); v != http.ErrAbortHandler { //nolint:errorlint // sentinel comparison
			t.Errorf("recovered %v, want http.ErrAbortHandler to pass through", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestMuxErrorsUsesTheEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /things", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"ok": "yes"})
	})
	h := RequestID(MuxErrors(mux))

	tests := []struct {
		method, path string
		status       int
		code         string
	}{
		{http.MethodGet, "/nothing-here", http.StatusNotFound, CodeNotFound},
		{http.MethodDelete, "/things", http.StatusMethodNotAllowed, CodeMethodNotAllowed},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
		var env envelopeJSON
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s %s: not JSON: %q", tt.method, tt.path, rec.Body.String())
		}
		if rec.Code != tt.status || env.Error.Code != tt.code || env.Error.RequestID == "" {
			t.Errorf("%s %s: got %d %+v", tt.method, tt.path, rec.Code, env.Error)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/things", nil))
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "GET") {
		t.Errorf("Allow header lost: %q", allow)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":"yes"`) {
		t.Errorf("normal responses must pass untouched: %d %s", rec.Code, rec.Body.String())
	}
}

// A handler's own 404 in JSON (e.g. "goal not found") must not be rewritten.
func TestMuxErrorsLeavesJSON404Alone(t *testing.T) {
	h := MuxErrors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, quietLogger(), NewError(http.StatusNotFound, "GOAL_NOT_FOUND", "No such goal."))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/goals/1", nil))
	if !strings.Contains(rec.Body.String(), "GOAL_NOT_FOUND") {
		t.Errorf("handler's own 404 was replaced: %s", rec.Body.String())
	}
}
