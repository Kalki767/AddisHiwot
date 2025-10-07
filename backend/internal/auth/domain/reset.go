package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidResetToken covers an unknown, expired or already used reset token.
// The client's answer is the same for all of them: start the reset again.
var ErrInvalidResetToken = errors.New("auth: invalid or expired reset token")

// ResetTokenTTL is how long the token from the code step can be used. It is
// short because, unlike a refresh token, it can change the password.
const ResetTokenTTL = 15 * time.Minute

// ResetToken is a usable password-reset token.
type ResetToken struct {
	ID     uuid.UUID
	UserID uuid.UUID
}

// ResetTokenRepository stores password-reset tokens (only their hashes).
type ResetTokenRepository interface {
	Create(ctx context.Context, userID uuid.UUID, hash []byte, now, expiresAt time.Time) error
	// Find returns the token if it is unused and not expired at now, else
	// ErrInvalidResetToken. It does not consume the token, so a request that
	// fails later (for example a password that breaks the rules) can be retried.
	Find(ctx context.Context, hash []byte, now time.Time) (ResetToken, error)
	// Consume marks the token used; ErrInvalidResetToken if another request was first.
	Consume(ctx context.Context, id uuid.UUID, now time.Time) error
}

// NewResetToken returns a new random reset token and the hash to store, as
// strong as a refresh token (256 random bits).
func NewResetToken() (token string, hash []byte) { return newOpaqueToken() }

// HashResetToken returns the SHA-256 of a reset token.
func HashResetToken(token string) []byte { return hashOpaqueToken(token) }
