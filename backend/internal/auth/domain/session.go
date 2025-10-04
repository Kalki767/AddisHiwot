package domain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Session and token errors.
var (
	// ErrInvalidRefreshToken covers an unknown, expired or revoked refresh token.
	// The client's answer is the same for all of them: log in again.
	ErrInvalidRefreshToken = errors.New("auth: invalid refresh token")
	// ErrRefreshTokenReused means a refresh token that was already exchanged was
	// presented again: it was copied, so its whole session is revoked.
	ErrRefreshTokenReused = errors.New("auth: refresh token reused")
	// ErrTokenExpired means an access token was valid but is past its expiry;
	// the client should refresh.
	ErrTokenExpired = errors.New("auth: access token expired")
	// ErrInvalidToken means an access token is malformed, forged or not ours.
	ErrInvalidToken = errors.New("auth: invalid access token")
)

// RevokeReason says why a session ended.
type RevokeReason string

// Reasons a session can be revoked; they match the database check constraint.
const (
	RevokeLogout         RevokeReason = "logout"
	RevokeReuseDetected  RevokeReason = "reuse_detected"
	RevokePasswordReset  RevokeReason = "password_reset"
	RevokeAccountDeleted RevokeReason = "account_deleted"
)

// RefreshTokenRecord is a stored refresh token with what is needed to judge it.
type RefreshTokenRecord struct {
	ID             uuid.UUID
	SessionID      uuid.UUID
	UserID         uuid.UUID
	ExpiresAt      time.Time
	Used           bool // already exchanged for a newer token
	SessionRevoked bool
}

// SessionRepository stores sessions and their refresh tokens. Only hashes of
// refresh tokens are ever stored.
type SessionRepository interface {
	// CreateSession starts a session for userID with its first refresh token.
	CreateSession(ctx context.Context, userID uuid.UUID, tokenHash []byte, expiresAt time.Time) (sessionID uuid.UUID, err error)
	// GetRefreshToken returns ErrInvalidRefreshToken when no token has this hash.
	GetRefreshToken(ctx context.Context, tokenHash []byte) (RefreshTokenRecord, error)
	// Rotate marks oldTokenID used and stores the next token of the session, in one
	// transaction. It returns ErrRefreshTokenReused if oldTokenID was already used,
	// including by a request racing with this one.
	Rotate(ctx context.Context, oldTokenID, sessionID uuid.UUID, newHash []byte, newExpiresAt time.Time) error
	// RevokeSession ends a session; revoking an ended session is not an error.
	RevokeSession(ctx context.Context, sessionID uuid.UUID, reason RevokeReason) error
}

// Claims are what an access token proves.
type Claims struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	ExpiresAt time.Time
}

// AccessTokens issues and checks short-lived access tokens.
type AccessTokens interface {
	Issue(userID, sessionID uuid.UUID, now time.Time) (token string, expiresAt time.Time, err error)
	// Verify returns ErrTokenExpired or ErrInvalidToken when the token is not usable at now.
	Verify(token string, now time.Time) (Claims, error)
}

// NewRefreshToken returns a new random refresh token (256 bits, URL-safe text)
// and the hash to store for it.
func NewRefreshToken() (token string, hash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never fails since Go 1.24
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashRefreshToken(token)
}

// HashRefreshToken returns the SHA-256 of a refresh token. A fast hash is right
// here: the token is random and long, so it cannot be guessed like a password.
func HashRefreshToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Principal is the logged-in user behind a request.
type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

type principalKey struct{}

// WithPrincipal stores p in ctx; the auth middleware calls it.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the logged-in user of a request that passed the auth middleware.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
