package validate

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatorCollectsEveryFieldSorted(t *testing.T) {
	var v Validator
	v.Check(false, "username", "too_short", "must have at least 3 characters")
	v.Check(true, "display_name", "required", "is required")
	v.Check(false, "email", "invalid_email", "must be an email address")
	err := v.Err()

	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("Err() = %v, want Errors", err)
	}
	if len(errs) != 2 || errs[0].Field != "email" || errs[1].Field != "username" {
		t.Fatalf("got %+v, want email then username", errs)
	}
	if !strings.Contains(err.Error(), "email: must be an email address") {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestValidatorKeepsFirstProblemPerField(t *testing.T) {
	var v Validator
	v.Check(false, "username", "required", "is required")
	v.Check(false, "username", "too_short", "must have at least 3 characters")
	var errs Errors
	if !errors.As(v.Err(), &errs) || len(errs) != 1 || errs[0].Code != "required" {
		t.Errorf("got %+v, want only the first problem", errs)
	}
}

func TestValidatorNoProblems(t *testing.T) {
	var v Validator
	v.Check(true, "email", "invalid_email", "x")
	if err := v.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestRunesCountCharactersNotBytes(t *testing.T) {
	amharic := strings.Repeat("ሀ", 50) // 150 bytes
	if !MaxRunes(amharic, 50) || MaxRunes(amharic, 49) {
		t.Error("MaxRunes must count 50 Amharic letters as 50")
	}
	if !MinRunes("ሀሁሂ", 3) || MinRunes("ሀሁ", 3) {
		t.Error("MinRunes must count letters")
	}
}

func TestNotBlank(t *testing.T) {
	for s, want := range map[string]bool{"": false, "   ": false, "\t\n": false, " a ": true, "ሀ": true} {
		if got := NotBlank(s); got != want {
			t.Errorf("NotBlank(%q) = %t, want %t", s, got, want)
		}
	}
}

func TestEmail(t *testing.T) {
	good := []string{"kidist@example.com", "k.d+tag@mail.example.et", "KIDIST@EXAMPLE.COM"}
	bad := []string{"", "kidist", "kidist@", "@example.com", "kidist@localhost", "a b@example.com",
		"Kidist <kidist@example.com>", "kidist@example.com.", "kidist@.example.com", "a@b@example.com",
		strings.Repeat("a", 250) + "@example.com"}
	for _, s := range good {
		if !Email(s) {
			t.Errorf("Email(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if Email(s) {
			t.Errorf("Email(%q) = true, want false", s)
		}
	}
}

func TestOneOf(t *testing.T) {
	if !OneOf("am", "en", "am") || OneOf("fr", "en", "am") || OneOf("") {
		t.Error("OneOf gave a wrong answer")
	}
}
