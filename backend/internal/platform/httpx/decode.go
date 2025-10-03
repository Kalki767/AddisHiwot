package httpx

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// MaxBodyBytes is the largest request body accepted (64 KB). Bigger bodies
// (audio, images) never go through JSON endpoints.
const MaxBodyBytes = 64 << 10

// DecodeJSON reads one JSON object from the request body into dst. It rejects
// a wrong Content-Type, an empty or malformed body, fields dst does not have,
// bodies over MaxBodyBytes and anything after the object. Every failure is an
// *Error ready for WriteError.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return NewError(http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "Send the body as application/json.")
	}

	body := bufio.NewReader(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	// json.Decode accepts a bare `null` and leaves dst untouched, which would
	// make `null` look like `{}`; only an object may start the body.
	if first, err := firstNonSpace(body); err == nil && first != '{' {
		return NewError(http.StatusBadRequest, CodeBadJSON, "The body must be a JSON object.")
	}
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if tooLarge(err) {
			return decodeError(err)
		}
		return NewError(http.StatusBadRequest, CodeBadJSON, "The body must contain a single JSON object.")
	}
	return nil
}

func decodeError(err error) error {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case tooLarge(err):
		return NewError(http.StatusRequestEntityTooLarge, CodeBodyTooLarge,
			fmt.Sprintf("The body must not be larger than %d KB.", MaxBodyBytes>>10))
	case errors.Is(err, io.EOF):
		return NewError(http.StatusBadRequest, CodeEmptyBody, "The body must not be empty.")
	case errors.As(err, &syntaxErr):
		return NewError(http.StatusBadRequest, CodeBadJSON, fmt.Sprintf("Malformed JSON at position %d.", syntaxErr.Offset))
	case errors.Is(err, io.ErrUnexpectedEOF):
		return NewError(http.StatusBadRequest, CodeBadJSON, "Malformed JSON: the body ends too early.")
	case errors.As(err, &typeErr):
		if typeErr.Field != "" {
			return NewError(http.StatusBadRequest, CodeBadJSON, fmt.Sprintf("Field %q must be a %s.", typeErr.Field, typeErr.Type))
		}
		return NewError(http.StatusBadRequest, CodeBadJSON, "The body must be a JSON object.")
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		field := strings.TrimPrefix(err.Error(), "json: unknown field ")
		return NewError(http.StatusBadRequest, CodeUnknownField, "Unknown field "+field+".")
	default:
		return NewError(http.StatusBadRequest, CodeBadJSON, "Malformed JSON.")
	}
}

// firstNonSpace returns the first byte after JSON white space without consuming it.
func firstNonSpace(r *bufio.Reader) (byte, error) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return b, r.UnreadByte()
		}
	}
}

func tooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
