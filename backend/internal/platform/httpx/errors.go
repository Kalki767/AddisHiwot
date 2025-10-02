package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/validate"
)

// Error codes. Clients switch on the code, never on the message.
const (
	CodeBadJSON              = "BAD_JSON"
	CodeEmptyBody            = "EMPTY_BODY"
	CodeUnknownField         = "UNKNOWN_FIELD"
	CodeBodyTooLarge         = "BODY_TOO_LARGE"
	CodeUnsupportedMediaType = "UNSUPPORTED_MEDIA_TYPE"
	CodeValidationFailed     = "VALIDATION_FAILED"
	CodeBadCursor            = "BAD_CURSOR"
	CodeNotFound             = "NOT_FOUND"
	CodeMethodNotAllowed     = "METHOD_NOT_ALLOWED"
	CodeInternal             = "INTERNAL"
)

// Error is an error with an HTTP status and a code a client can act on.
type Error struct {
	Status  int
	Code    string
	Message string
	Fields  []validate.FieldError
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// NewError returns an *Error.
func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// envelope is the one shape every error response has:
//
//	{"error": {"code": "...", "message": "...", "request_id": "...", "fields": [...]}}
type envelope struct {
	Error envelopeBody `json:"error"`
}

type envelopeBody struct {
	Code      string                `json:"code"`
	Message   string                `json:"message"`
	RequestID string                `json:"request_id"`
	Fields    []validate.FieldError `json:"fields,omitempty"`
}

// WriteError sends err in the error envelope.
//   - *Error is sent as it is.
//   - validate.Errors becomes 422 VALIDATION_FAILED with the field list.
//   - anything else is a bug or an outage: it is logged with the request ID
//     and the client gets a plain 500, never the internal message.
func WriteError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var he *Error
	var fields validate.Errors
	switch {
	case errors.As(err, &he):
	case errors.As(err, &fields):
		he = &Error{Status: http.StatusUnprocessableEntity, Code: CodeValidationFailed,
			Message: "Some fields are not valid.", Fields: fields}
	default:
		logger.ErrorContext(r.Context(), "internal error",
			slog.String("request_id", RequestIDFrom(r.Context())), slog.Any("error", err))
		he = &Error{Status: http.StatusInternalServerError, Code: CodeInternal, Message: "Something went wrong. Please try again."}
	}
	writeEnvelope(w, r, he)
}

func writeEnvelope(w http.ResponseWriter, r *http.Request, he *Error) {
	WriteJSON(w, he.Status, envelope{Error: envelopeBody{
		Code: he.Code, Message: he.Message, RequestID: RequestIDFrom(r.Context()), Fields: he.Fields,
	}})
}

// WriteJSON sends v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
