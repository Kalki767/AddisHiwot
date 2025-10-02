package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/validate"
)

func quietLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

type envelopeJSON struct {
	Error struct {
		Code      string                `json:"code"`
		Message   string                `json:"message"`
		RequestID string                `json:"request_id"`
		Fields    []validate.FieldError `json:"fields"`
	} `json:"error"`
}

func writeErr(t *testing.T, logger *slog.Logger, err error) (*httptest.ResponseRecorder, envelopeJSON) {
	t.Helper()
	rec := httptest.NewRecorder()
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { WriteError(w, r, logger, err) }))
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var env envelopeJSON
	if e := json.Unmarshal(rec.Body.Bytes(), &env); e != nil {
		t.Fatalf("not JSON: %s", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if env.Error.RequestID == "" || env.Error.RequestID != rec.Header().Get(RequestIDHeader) {
		t.Errorf("request_id %q does not match header %q", env.Error.RequestID, rec.Header().Get(RequestIDHeader))
	}
	return rec, env
}

func TestWriteErrorSendsHTTPErrorAsIs(t *testing.T) {
	rec, env := writeErr(t, quietLogger(), NewError(http.StatusTeapot, "TEAPOT", "Short and stout."))
	if rec.Code != http.StatusTeapot || env.Error.Code != "TEAPOT" || env.Error.Message != "Short and stout." {
		t.Errorf("got %d %+v", rec.Code, env.Error)
	}
	if env.Error.Fields != nil {
		t.Errorf("fields must be left out when empty, got %v", env.Error.Fields)
	}
}

func TestWriteErrorTurnsValidationIntoA422WithFields(t *testing.T) {
	var v validate.Validator
	v.Check(false, "username", "too_short", "must have at least 3 characters")
	v.Check(false, "email", "invalid_email", "must be an email address")
	wrapped := fmt.Errorf("signup: %w", v.Err())

	rec, env := writeErr(t, quietLogger(), wrapped)
	if rec.Code != http.StatusUnprocessableEntity || env.Error.Code != CodeValidationFailed {
		t.Fatalf("got %d %s", rec.Code, env.Error.Code)
	}
	if len(env.Error.Fields) != 2 || env.Error.Fields[0].Field != "email" || env.Error.Fields[1].Code != "too_short" {
		t.Errorf("fields = %+v", env.Error.Fields)
	}
}

func TestWriteErrorHidesInternalErrorsButLogsThem(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	rec, env := writeErr(t, logger, errors.New("pq: password authentication failed for user admin"))

	if rec.Code != http.StatusInternalServerError || env.Error.Code != CodeInternal {
		t.Fatalf("got %d %s", rec.Code, env.Error.Code)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("internal detail leaked to the client: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "password authentication failed") || !strings.Contains(logs.String(), env.Error.RequestID) {
		t.Errorf("log must hold the cause and the request ID: %s", logs.String())
	}
}
