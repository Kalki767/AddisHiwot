// Package postgres implements the auth domain's SessionRepository with
// PostgreSQL, using the queries sqlc generates in internal/db.
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

// The compiler checks that Sessions keeps matching the domain interface.
var _ domain.SessionRepository = (*Sessions)(nil)

// Conn is a pool or connection that can also start transactions (*pgxpool.Pool is one).
type Conn interface {
	db.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Sessions stores sessions and refresh-token hashes.
type Sessions struct {
	conn Conn
	q    *db.Queries
}

// NewSessions returns a SessionRepository on conn.
func NewSessions(conn Conn) *Sessions {
	return &Sessions{conn: conn, q: db.New(conn)}
}

// CreateSession starts a session with its first refresh token, in one transaction.
func (s *Sessions) CreateSession(ctx context.Context, userID uuid.UUID, tokenHash []byte, expiresAt time.Time) (uuid.UUID, error) {
	var sessionID uuid.UUID
	err := s.inTx(ctx, func(q *db.Queries) error {
		session, err := q.CreateSession(ctx, userID)
		if err != nil {
			return err
		}
		sessionID = session.ID
		return q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{SessionID: session.ID, TokenHash: tokenHash, ExpiresAt: expiresAt})
	})
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("auth: create session: %w", err)
	}
	return sessionID, nil
}

// GetRefreshToken finds a token by its hash.
func (s *Sessions) GetRefreshToken(ctx context.Context, tokenHash []byte) (domain.RefreshTokenRecord, error) {
	row, err := s.q.GetRefreshToken(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RefreshTokenRecord{}, domain.ErrInvalidRefreshToken
	}
	if err != nil {
		return domain.RefreshTokenRecord{}, fmt.Errorf("auth: get refresh token: %w", err)
	}
	return domain.RefreshTokenRecord{
		ID: row.ID, SessionID: row.SessionID, UserID: row.UserID, ExpiresAt: row.ExpiresAt,
		Used: row.UsedAt != nil, SessionRevoked: row.RevokedAt != nil,
	}, nil
}

// Rotate marks the old token used and stores the next one. The UPDATE only
// succeeds for a token not used yet, so of two requests racing with the same
// token exactly one wins; the other gets ErrRefreshTokenReused.
func (s *Sessions) Rotate(ctx context.Context, oldTokenID, sessionID uuid.UUID, newHash []byte, newExpiresAt time.Time) error {
	err := s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.MarkRefreshTokenUsed(ctx, oldTokenID)
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrRefreshTokenReused
		}
		if err := q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{SessionID: sessionID, TokenHash: newHash, ExpiresAt: newExpiresAt}); err != nil {
			return err
		}
		return q.TouchSession(ctx, sessionID)
	})
	if err != nil && !errors.Is(err, domain.ErrRefreshTokenReused) {
		return fmt.Errorf("auth: rotate refresh token: %w", err)
	}
	return err
}

// RevokeSession ends a session; an already ended session is left as it is.
func (s *Sessions) RevokeSession(ctx context.Context, sessionID uuid.UUID, reason domain.RevokeReason) error {
	if _, err := s.q.RevokeSession(ctx, db.RevokeSessionParams{ID: sessionID, Reason: ptr(string(reason))}); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

func (s *Sessions) inTx(ctx context.Context, fn func(*db.Queries) error) error {
	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck // a no-op after Commit
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ptr[T any](v T) *T { return &v }
