package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/db"
)

// The compiler checks that ResetTokens keeps matching the domain interface.
var _ domain.ResetTokenRepository = (*ResetTokens)(nil)

// ResetTokens stores password-reset tokens.
type ResetTokens struct {
	q *db.Queries
}

// NewResetTokens returns a ResetTokenRepository on conn.
func NewResetTokens(conn db.DBTX) *ResetTokens { return &ResetTokens{q: db.New(conn)} }

// Create stores a new token.
func (r *ResetTokens) Create(ctx context.Context, userID uuid.UUID, hash []byte, now, expiresAt time.Time) error {
	err := r.q.CreateResetToken(ctx, db.CreateResetTokenParams{UserID: userID, TokenHash: hash, CreatedAt: now, ExpiresAt: expiresAt})
	if err != nil {
		return fmt.Errorf("auth: create reset token: %w", err)
	}
	return nil
}

// Find returns a token that is unused and not expired.
func (r *ResetTokens) Find(ctx context.Context, hash []byte, now time.Time) (domain.ResetToken, error) {
	row, err := r.q.FindResetToken(ctx, db.FindResetTokenParams{TokenHash: hash, Now: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ResetToken{}, domain.ErrInvalidResetToken
	}
	if err != nil {
		return domain.ResetToken{}, fmt.Errorf("auth: find reset token: %w", err)
	}
	return domain.ResetToken{ID: row.ID, UserID: row.UserID}, nil
}

// Consume marks the token used; only the first caller succeeds.
func (r *ResetTokens) Consume(ctx context.Context, id uuid.UUID, now time.Time) error {
	n, err := r.q.ConsumeResetToken(ctx, db.ConsumeResetTokenParams{ID: id, Now: &now})
	if err != nil {
		return fmt.Errorf("auth: consume reset token: %w", err)
	}
	if n == 0 {
		return domain.ErrInvalidResetToken
	}
	return nil
}
