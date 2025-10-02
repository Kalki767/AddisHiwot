package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type signupBody struct {
	Email string `json:"email"`
	Age   int    `json:"age"`
}

func decode(contentType, body string) (signupBody, error) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	var dst signupBody
	err := DecodeJSON(httptest.NewRecorder(), req, &dst)
	return dst, err
}

func TestDecodeJSONAcceptsAValidObject(t *testing.T) {
	got, err := decode("application/json; charset=utf-8", `{"email":"k@example.com","age":30}`)
	if err != nil || got.Email != "k@example.com" || got.Age != 30 {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestDecodeJSONErrorPaths(t *testing.T) {
	tests := []struct {
		name, contentType, body string
		status                  int
		code, inMessage         string
	}{
		{"bad JSON", "application/json", `{"email": "k@example.com",}`, 400, CodeBadJSON, "position"},
		{"body ends early", "application/json", `{"email": "k@`, 400, CodeBadJSON, "ends too early"},
		{"wrong type", "application/json", `{"age": "thirty"}`, 400, CodeBadJSON, `"age" must be a int`},
		{"not an object", "application/json", `["a"]`, 400, CodeBadJSON, "JSON object"},
		{"two objects", "application/json", `{"age":1}{"age":2}`, 400, CodeBadJSON, "single JSON object"},
		{"unknown field", "application/json", `{"email":"k@example.com","is_admin":true}`, 400, CodeUnknownField, `"is_admin"`},
		{"empty body", "application/json", ``, 400, CodeEmptyBody, "empty"},
		{"too large", "application/json", `{"email":"` + strings.Repeat("a", MaxBodyBytes) + `"}`, 413, CodeBodyTooLarge, "64 KB"},
		{"too large after a valid object", "application/json", `{"age":1}` + strings.Repeat(" ", MaxBodyBytes), 413, CodeBodyTooLarge, "64 KB"},
		{"no content type", "", `{"age":1}`, 415, CodeUnsupportedMediaType, "application/json"},
		{"form content type", "application/x-www-form-urlencoded", `age=1`, 415, CodeUnsupportedMediaType, "application/json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decode(tt.contentType, tt.body)
			var he *Error
			if !errors.As(err, &he) {
				t.Fatalf("err = %v, want *Error", err)
			}
			if he.Status != tt.status || he.Code != tt.code || !strings.Contains(he.Message, tt.inMessage) {
				t.Errorf("got %d %s %q, want %d %s containing %q", he.Status, he.Code, he.Message, tt.status, tt.code, tt.inMessage)
			}
		})
	}
}

// The full path a handler takes: decode fails, WriteError sends the envelope.
func TestDecodeErrorBecomesEnvelope(t *testing.T) {
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var dst signupBody
		if err := DecodeJSON(w, r, &dst); err != nil {
			WriteError(w, r, quietLogger(), err)
		}
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"nope":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(RequestIDHeader, "req-7")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	var env envelopeJSON
	body := rec.Body.Bytes()
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("body is not JSON: %s", body)
	}
	if env.Error.Code != CodeUnknownField || env.Error.RequestID != "req-7" {
		t.Errorf("envelope = %s, want UNKNOWN_FIELD with request_id req-7", body)
	}
}
