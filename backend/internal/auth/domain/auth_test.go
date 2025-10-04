package domain

import (
	"errors"
	"strings"
	"testing"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/validate"
)

func passwordProblem(password, username, email string) string {
	var v validate.Validator
	CheckPassword(&v, password, username, email)
	var errs validate.Errors
	if !errors.As(v.Err(), &errs) {
		return ""
	}
	return errs[0].Code
}

func TestCheckPassword(t *testing.T) {
	tests := []struct{ password, want string }{
		{"correct horse battery", ""},
		{"ሰላም ለኢትዮጵያ", ""}, // 10 characters, 28 bytes
		{"short", "too_short"},
		{"ሰላምለሁላችሁ", ""},         // 8 characters (24 bytes) is enough
		{"ሰላምለሁላች", "too_short"}, // 7 characters, even though 21 bytes
		{strings.Repeat("a", 129), "too_long"},
		{"Password", "too_common"},
		{"12345678", "too_common"},
		{"AddisHiwot", "too_common"},
		{"my-kidista-2026", "contains_username"},
		{"kidist.t-was-here", "contains_email"},
	}
	for _, tt := range tests {
		if got := passwordProblem(tt.password, "KidistA", "kidist.t@example.com"); got != tt.want {
			t.Errorf("CheckPassword(%q) = %q, want %q", tt.password, got, tt.want)
		}
	}
}

func TestCheckPasswordIgnoresShortEmailNames(t *testing.T) {
	// A two-letter local part ("ab@…") would forbid far too many passwords.
	if got := passwordProblem("lab-coat-yellow", "kidist", "ab@example.com"); got != "" {
		t.Errorf("got %q, want no problem", got)
	}
}

func TestIsReservedUsername(t *testing.T) {
	for _, name := range []string{"admin", "AddisBot", "SUPPORT", " moderator ", "addis_hiwot"} {
		if !IsReservedUsername(name) {
			t.Errorf("%q is not reserved, want reserved", name)
		}
	}
	for _, name := range []string{"kidist", "admins", "supporter", "addisbot2"} {
		if IsReservedUsername(name) {
			t.Errorf("%q is reserved, want free", name)
		}
	}
}
