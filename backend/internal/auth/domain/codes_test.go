package domain

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/google/uuid"
)

func TestNewCodeIsFiveDigits(t *testing.T) {
	five := regexp.MustCompile(`^[0-9]{5}$`)
	leadingZero := false
	for range 2000 {
		c := NewCode()
		if !five.MatchString(c) {
			t.Fatalf("code %q is not 5 digits", c)
		}
		if c[0] == '0' {
			leadingZero = true
		}
	}
	// About 1 code in 10 starts with 0; in 2,000 codes none would be a broken generator.
	if !leadingZero {
		t.Error("no code started with 0: codes are not drawn from the full range")
	}
}

func TestHashCodeIsBoundToPurposeAndPerson(t *testing.T) {
	secret := []byte("code-secret-that-is-32-bytes-long!")
	a, b := uuid.New(), uuid.New()
	h := HashCode(secret, PurposeVerifyEmail, a, "12345")
	if len(h) != 32 {
		t.Fatalf("hash has %d bytes", len(h))
	}
	for name, other := range map[string][]byte{
		"other person":  HashCode(secret, PurposeVerifyEmail, b, "12345"),
		"other purpose": HashCode(secret, PurposeResetPassword, a, "12345"),
		"other code":    HashCode(secret, PurposeVerifyEmail, a, "12346"),
		"other secret":  HashCode([]byte("another-secret-also-32-bytes-long!"), PurposeVerifyEmail, a, "12345"),
	} {
		if bytes.Equal(h, other) {
			t.Errorf("%s gives the same hash", name)
		}
	}
	if !CodeMatches(secret, PurposeVerifyEmail, a, "12345", h) || CodeMatches(secret, PurposeVerifyEmail, a, "54321", h) {
		t.Error("CodeMatches gave a wrong answer")
	}
}
