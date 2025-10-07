// Package postgres implements the users domain's Repository with PostgreSQL,
// using the queries sqlc generates in internal/db.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Kalki767/AddisHiwot/backend/internal/db"
	"github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

// The compiler checks that Repository keeps matching the domain interface.
var _ domain.Repository = (*Repository)(nil)

// Repository reads and writes users in PostgreSQL.
type Repository struct {
	q *db.Queries
}

// NewRepository works on a pool, a connection or a transaction.
func NewRepository(conn db.DBTX) *Repository {
	return &Repository{q: db.New(conn)}
}

// Create inserts a user.
func (r *Repository) Create(ctx context.Context, u domain.NewUser) (domain.User, error) {
	row, err := r.q.CreateUser(ctx, db.CreateUserParams{
		Email:            u.Email,
		Username:         u.Username,
		PasswordHash:     u.PasswordHash,
		DisplayName:      u.DisplayName,
		Locale:           u.Locale,
		Timezone:         u.Timezone,
		AdultConfirmedAt: u.AdultConfirmedAt,
		TermsAcceptedAt:  u.TermsAcceptedAt,
		TermsVersion:     u.TermsVersion,
	})
	if err != nil {
		return domain.User{}, mapError(err)
	}
	return toDomain(row), nil
}

// GetByID finds a user by id.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if err != nil {
		return domain.User{}, mapError(err)
	}
	return toDomain(row), nil
}

// GetByEmail finds a user by email in any letter case (the column is citext).
func (r *Repository) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		return domain.User{}, mapError(err)
	}
	return toDomain(row), nil
}

// GetByUsername finds a user by username in any letter case (the column is citext).
func (r *Repository) GetByUsername(ctx context.Context, username string) (domain.User, error) {
	row, err := r.q.GetUserByUsername(ctx, username)
	if err != nil {
		return domain.User{}, mapError(err)
	}
	return toDomain(row), nil
}

// MarkEmailVerified records the first verification time.
func (r *Repository) MarkEmailVerified(ctx context.Context, id uuid.UUID, at time.Time) error {
	if err := r.q.MarkEmailVerified(ctx, db.MarkEmailVerifiedParams{ID: id, VerifiedAt: &at}); err != nil {
		return fmt.Errorf("users: mark email verified: %w", err)
	}
	return nil
}

// SetPasswordHash replaces the stored password hash.
func (r *Repository) SetPasswordHash(ctx context.Context, id uuid.UUID, hash string) error {
	if err := r.q.UpdatePasswordHash(ctx, db.UpdatePasswordHashParams{ID: id, PasswordHash: &hash}); err != nil {
		return fmt.Errorf("users: set password hash: %w", err)
	}
	return nil
}

// PostgreSQL error codes (https://www.postgresql.org/docs/current/errcodes-appendix.html).
const (
	uniqueViolation = "23505"
	checkViolation  = "23514"
)

// mapError turns database errors into domain errors, so nothing outside this
// package needs to know about pgx.
func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == uniqueViolation && pgErr.ConstraintName == "users_email_key":
			return domain.ErrEmailTaken
		case pgErr.Code == uniqueViolation && pgErr.ConstraintName == "users_username_key":
			return domain.ErrUsernameTaken
		case pgErr.Code == checkViolation:
			return fmt.Errorf("%w: %s", domain.ErrInvalid, pgErr.ConstraintName)
		}
	}
	return fmt.Errorf("users: %w", err)
}

func toDomain(r db.User) domain.User {
	return domain.User{
		ID:               r.ID,
		Email:            r.Email,
		Username:         r.Username,
		PasswordHash:     r.PasswordHash,
		DisplayName:      r.DisplayName,
		Locale:           r.Locale,
		Timezone:         r.Timezone,
		AdultConfirmedAt: r.AdultConfirmedAt,
		TermsAcceptedAt:  r.TermsAcceptedAt,
		TermsVersion:     r.TermsVersion,
		EmailVerifiedAt:  r.EmailVerifiedAt,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}
