// Package jwttoken issues and checks access tokens as JWTs signed with HS256.
//
// An access token lives a short time (15 minutes by default) and is checked
// without touching the database: the signature proves the server issued it.
// Verification accepts only HS256, our issuer and our audience, and requires
// an expiry, so a token signed with another algorithm (or "none") is refused.
package jwttoken

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
)

// The compiler checks that Issuer keeps matching the domain interface.
var _ domain.AccessTokens = (*Issuer)(nil)

const (
	issuer   = "addishiwot-api"
	audience = "addishiwot-app"
	// MinSecretBytes is the shortest accepted signing secret (256 bits for HS256).
	MinSecretBytes = 32
)

// Issuer signs and verifies access tokens with one secret.
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

// New returns an Issuer. secret must have at least MinSecretBytes bytes.
func New(secret []byte, ttl time.Duration) (*Issuer, error) {
	if len(secret) < MinSecretBytes {
		return nil, fmt.Errorf("jwttoken: secret has %d bytes, need at least %d", len(secret), MinSecretBytes)
	}
	if ttl <= 0 {
		return nil, errors.New("jwttoken: ttl must be positive")
	}
	return &Issuer{secret: secret, ttl: ttl}, nil
}

type claims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

// Issue returns a signed token for the user's session and its expiry time.
func (i *Issuer) Issue(userID, sessionID uuid.UUID, now time.Time) (string, time.Time, error) {
	exp := now.Add(i.ttl)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		SessionID: sessionID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	})
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("jwttoken: sign: %w", err)
	}
	return signed, exp.Truncate(time.Second), nil
}

// Verify checks the token as of now and returns its claims, or
// domain.ErrTokenExpired / domain.ErrInvalidToken.
func (i *Issuer) Verify(token string, now time.Time) (domain.Claims, error) {
	var c claims
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	_, err := parser.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return i.secret, nil })
	if errors.Is(err, jwt.ErrTokenExpired) {
		return domain.Claims{}, domain.ErrTokenExpired
	}
	if err != nil {
		return domain.Claims{}, domain.ErrInvalidToken
	}
	userID, err1 := uuid.Parse(c.Subject)
	sessionID, err2 := uuid.Parse(c.SessionID)
	if err1 != nil || err2 != nil {
		return domain.Claims{}, domain.ErrInvalidToken
	}
	return domain.Claims{UserID: userID, SessionID: sessionID, ExpiresAt: c.ExpiresAt.Time}, nil
}
