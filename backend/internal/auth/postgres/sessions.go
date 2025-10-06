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
		_, err = q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{SessionID: session.ID, TokenHash: tokenHash, ExpiresAt: expiresAt})
		return err
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
		UsedAt: row.UsedAt, SuccessorID: row.ReplacedBy, SuccessorUsed: row.SuccessorUsed,
		SessionRevoked: row.RevokedAt != nil,
	}, nil
}

// Rotate stores the next token and marks the old one used and replaced by it.
// The UPDATE only succeeds for a token not used yet, so of two requests racing
// with the same token exactly one wins; the other's transaction rolls back
// (its new token disappears) and it gets ErrRefreshTokenReused.
func (s *Sessions) Rotate(ctx context.Context, oldTokenID, sessionID uuid.UUID, newHash []byte, newExpiresAt, now time.Time) error {
	err := s.inTx(ctx, func(q *db.Queries) error {
		newID, err := q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{SessionID: sessionID, TokenHash: newHash, ExpiresAt: newExpiresAt})
		if err != nil {
			return err
		}
		if err := markUsed(ctx, q, oldTokenID, now, &newID); err != nil {
			return err
		}
		return q.TouchSession(ctx, sessionID)
	})
	return wrap("rotate", err)
}

// Reissue answers a retry: the undelivered successor is used up with no
// successor of its own, and the new token becomes oldTokenID's successor.
func (s *Sessions) Reissue(ctx context.Context, oldTokenID, successorID, sessionID uuid.UUID, newHash []byte, newExpiresAt, now time.Time) error {
	err := s.inTx(ctx, func(q *db.Queries) error {
		newID, err := q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{SessionID: sessionID, TokenHash: newHash, ExpiresAt: newExpiresAt})
		if err != nil {
			return err
		}
		if err := markUsed(ctx, q, successorID, now, nil); err != nil {
			return err
		}
		if err := q.SetRefreshTokenReplacedBy(ctx, db.SetRefreshTokenReplacedByParams{ID: oldTokenID, ReplacedBy: &newID}); err != nil {
			return err
		}
		return q.TouchSession(ctx, sessionID)
	})
	return wrap("reissue", err)
}

// markUsed uses up an unused token; ErrRefreshTokenReused if it was already used.
func markUsed(ctx context.Context, q *db.Queries, id uuid.UUID, now time.Time, replacedBy *uuid.UUID) error {
	n, err := q.MarkRefreshTokenUsed(ctx, db.MarkRefreshTokenUsedParams{ID: id, UsedAt: &now, ReplacedBy: replacedBy})
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrRefreshTokenReused
	}
	return nil
}

func wrap(op string, err error) error {
	if err != nil && !errors.Is(err, domain.ErrRefreshTokenReused) {
		return fmt.Errorf("auth: %s refresh token: %w", op, err)
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
