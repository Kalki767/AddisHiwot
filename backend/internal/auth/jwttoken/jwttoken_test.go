package jwttoken

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
)

var (
	secret = []byte("test-secret-that-is-at-least-32-bytes-long")
	t0     = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
)

func newIssuer(t *testing.T) *Issuer {
	t.Helper()
	i, err := New(secret, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestIssueAndVerify(t *testing.T) {
	i := newIssuer(t)
	user, session := uuid.New(), uuid.New()
	token, exp, err := i.Issue(user, session, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(t0.Add(15 * time.Minute)) {
		t.Errorf("expiry = %v", exp)
	}
	c, err := i.Verify(token, t0.Add(14*time.Minute))
	if err != nil || c.UserID != user || c.SessionID != session || !c.ExpiresAt.Equal(exp) {
		t.Errorf("Verify = %+v, %v", c, err)
	}
}

func TestExpiredTokenIsTokenExpired(t *testing.T) {
	i := newIssuer(t)
	token, _, _ := i.Issue(uuid.New(), uuid.New(), t0)
	if _, err := i.Verify(token, t0.Add(15*time.Minute+time.Second)); !errors.Is(err, domain.ErrTokenExpired) {
		t.Errorf("err = %v, want ErrTokenExpired", err)
	}
}

func TestTokenFromTheFutureIsInvalid(t *testing.T) {
	i := newIssuer(t)
	token, _, _ := i.Issue(uuid.New(), uuid.New(), t0)
	if _, err := i.Verify(token, t0.Add(-time.Minute)); !errors.Is(err, domain.ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken for a token issued after now", err)
	}
}

func sign(t *testing.T, method jwt.SigningMethod, key any, c jwt.Claims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, c).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func goodClaims() claims {
	return claims{SessionID: uuid.NewString(), RegisteredClaims: jwt.RegisteredClaims{
		Issuer: issuer, Subject: uuid.NewString(), Audience: jwt.ClaimStrings{audience},
		IssuedAt: jwt.NewNumericDate(t0), ExpiresAt: jwt.NewNumericDate(t0.Add(time.Hour)),
	}}
}

func TestRejectsForgedAndForeignTokens(t *testing.T) {
	i := newIssuer(t)
	valid, _, _ := i.Issue(uuid.New(), uuid.New(), t0)
	parts := strings.Split(valid, ".")

	noExp := goodClaims()
	noExp.ExpiresAt = nil
	wrongIss, wrongAud, badSub := goodClaims(), goodClaims(), goodClaims()
	wrongIss.Issuer = "someone-else"
	wrongAud.Audience = jwt.ClaimStrings{"other-app"}
	badSub.Subject = "not-a-uuid"

	tests := map[string]string{
		"garbage":            "not.a.token",
		"other secret":       sign(t, jwt.SigningMethodHS256, []byte("another-secret-also-32-bytes-long!!"), goodClaims()),
		"HS512 same secret":  sign(t, jwt.SigningMethodHS512, secret, goodClaims()),
		"alg none":           sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, goodClaims()),
		"tampered payload":   parts[0] + "." + strings.TrimRight(parts[1], "A") + "B." + parts[2],
		"no expiry":          sign(t, jwt.SigningMethodHS256, secret, noExp),
		"wrong issuer":       sign(t, jwt.SigningMethodHS256, secret, wrongIss),
		"wrong audience":     sign(t, jwt.SigningMethodHS256, secret, wrongAud),
		"subject not a uuid": sign(t, jwt.SigningMethodHS256, secret, badSub),
	}
	for name, token := range tests {
		if _, err := i.Verify(token, t0.Add(time.Minute)); !errors.Is(err, domain.ErrInvalidToken) {
			t.Errorf("%s: err = %v, want ErrInvalidToken", name, err)
		}
	}
}

func TestNewRejectsWeakSettings(t *testing.T) {
	if _, err := New([]byte("short"), time.Minute); err == nil {
		t.Error("a 5-byte secret was accepted")
	}
	if _, err := New(secret, 0); err == nil {
		t.Error("a zero ttl was accepted")
	}
}
