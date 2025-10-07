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

// The compiler checks that Codes keeps matching the domain interface.
var _ domain.CodeRepository = (*Codes)(nil)

// Codes stores email codes (only their HMAC).
type Codes struct {
	conn Conn
	q    *db.Queries
}

// NewCodes returns a CodeRepository on conn.
func NewCodes(conn Conn) *Codes { return &Codes{conn: conn, q: db.New(conn)} }

// Issue retires the live code and stores the new one in one transaction.
func (c *Codes) Issue(ctx context.Context, userID uuid.UUID, purpose domain.CodePurpose, hash []byte, now, expiresAt time.Time) error {
	tx, err := c.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: issue code: %w", err)
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck // a no-op after Commit
	q := c.q.WithTx(tx)
	if err := q.LockUserCodes(ctx, userID.String()+"/"+string(purpose)); err != nil {
		return fmt.Errorf("auth: lock codes: %w", err)
	}
	if err := q.RetireLiveCodes(ctx, db.RetireLiveCodesParams{UserID: userID, Purpose: string(purpose), Now: &now}); err != nil {
		return fmt.Errorf("auth: retire codes: %w", err)
	}
	if err := q.CreateEmailCode(ctx, db.CreateEmailCodeParams{UserID: userID, Purpose: string(purpose), CodeHash: hash, Now: now, ExpiresAt: expiresAt}); err != nil {
		return fmt.Errorf("auth: create code: %w", err)
	}
	return tx.Commit(ctx)
}

// CountSince counts codes issued since a time.
func (c *Codes) CountSince(ctx context.Context, userID uuid.UUID, purpose domain.CodePurpose, since time.Time) (int, error) {
	n, err := c.q.CountEmailCodesSince(ctx, db.CountEmailCodesSinceParams{UserID: userID, Purpose: string(purpose), Since: since})
	if err != nil {
		return 0, fmt.Errorf("auth: count codes: %w", err)
	}
	return int(n), nil
}

// UseAttempt counts an attempt and returns the code to compare, in one UPDATE.
func (c *Codes) UseAttempt(ctx context.Context, userID uuid.UUID, purpose domain.CodePurpose, now time.Time, maxAttempts int) (domain.CodeAttempt, error) {
	row, err := c.q.UseEmailCodeAttempt(ctx, db.UseEmailCodeAttemptParams{
		UserID: userID, Purpose: string(purpose), Now: now, MaxAttempts: int32(min(maxAttempts, 1000)), //nolint:gosec // G115: capped at 1000
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CodeAttempt{}, domain.ErrNoLiveCode
	}
	if err != nil {
		return domain.CodeAttempt{}, fmt.Errorf("auth: use code attempt: %w", err)
	}
	return domain.CodeAttempt{ID: row.ID, Hash: row.CodeHash}, nil
}

// Consume marks the code used; only the first caller succeeds.
func (c *Codes) Consume(ctx context.Context, id uuid.UUID, now time.Time) error {
	n, err := c.q.ConsumeEmailCode(ctx, db.ConsumeEmailCodeParams{ID: id, Now: &now})
	if err != nil {
		return fmt.Errorf("auth: consume code: %w", err)
	}
	if n == 0 {
		return domain.ErrCodeAlreadyUsed
	}
	return nil
}
