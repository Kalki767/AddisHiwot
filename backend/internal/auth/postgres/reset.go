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
	conn Conn
	q    *db.Queries
}

// NewResetTokens returns a ResetTokenRepository on conn.
func NewResetTokens(conn Conn) *ResetTokens { return &ResetTokens{conn: conn, q: db.New(conn)} }

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

// Complete spends the token, sets the password, ends every session and marks
// the email verified, in one transaction.
func (r *ResetTokens) Complete(ctx context.Context, t domain.ResetToken, passwordHash string, now time.Time) error {
	err := inTx(ctx, r.conn, r.q, func(q *db.Queries) error {
		n, err := q.ConsumeResetToken(ctx, db.ConsumeResetTokenParams{ID: t.ID, Now: &now})
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrInvalidResetToken
		}
		if err := q.UpdatePasswordHash(ctx, db.UpdatePasswordHashParams{ID: t.UserID, PasswordHash: &passwordHash}); err != nil {
			return err
		}
		reason := string(domain.RevokePasswordReset)
		if _, err := q.RevokeUserSessions(ctx, db.RevokeUserSessionsParams{UserID: t.UserID, Reason: &reason, Now: &now}); err != nil {
			return err
		}
		return q.MarkEmailVerified(ctx, db.MarkEmailVerifiedParams{ID: t.UserID, VerifiedAt: &now})
	})
	if err != nil && !errors.Is(err, domain.ErrInvalidResetToken) {
		return fmt.Errorf("auth: complete password reset: %w", err)
	}
	return err
}
