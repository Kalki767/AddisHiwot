// Package validate collects every problem with an input at once, so a client
// can show all of them together instead of fixing one field per round trip.
//
// Each problem has a stable machine code (for the app to translate into
// English or Amharic) and an English message (for logs and developers).
// The package has no HTTP or database code, so domain and use-case code may use it.
package validate

import (
	"net/mail"
	"sort"
	"strings"
	"unicode/utf8"
)

// FieldError is one problem with one field.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Errors is a list of field problems; it is an error, so it can be returned as one.
type Errors []FieldError

func (e Errors) Error() string {
	parts := make([]string, len(e))
	for i, f := range e {
		parts[i] = f.Field + ": " + f.Message
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// Validator gathers problems. The zero value is ready to use.
type Validator struct {
	errs Errors
}

// Check records a problem when ok is false. Only the first problem per field
// is kept, so a blank field does not also report "too short".
func (v *Validator) Check(ok bool, field, code, message string) {
	if ok {
		return
	}
	for _, e := range v.errs {
		if e.Field == field {
			return
		}
	}
	v.errs = append(v.errs, FieldError{Field: field, Code: code, Message: message})
}

// Err returns nil when every check passed, otherwise Errors sorted by field.
func (v *Validator) Err() error {
	if len(v.errs) == 0 {
		return nil
	}
	out := append(Errors(nil), v.errs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

// NotBlank reports whether s has a character other than white space.
func NotBlank(s string) bool { return strings.TrimSpace(s) != "" }

// MaxRunes reports whether s has at most n characters. Characters, not bytes:
// an Amharic letter is one character but three bytes.
func MaxRunes(s string, n int) bool { return utf8.RuneCountInString(s) <= n }

// MinRunes reports whether s has at least n characters.
func MinRunes(s string, n int) bool { return utf8.RuneCountInString(s) >= n }

// Email reports whether s is a plain address like name@example.com: one '@',
// no display name or angle brackets, and a dot in the domain.
func Email(s string) bool {
	if len(s) > 254 || strings.ContainsAny(s, " \t\r\n<>") {
		return false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || addr.Name != "" {
		return false
	}
	at := strings.LastIndexByte(s, '@')
	domain := s[at+1:]
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

// OneOf reports whether s equals one of the allowed values.
func OneOf(s string, allowed ...string) bool {
	for _, a := range allowed {
		if s == a {
			return true
		}
	}
	return false
}
