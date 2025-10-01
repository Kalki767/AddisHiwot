// Package domain is the core of the users feature: the account entity, its
// errors and the storage interface it needs.
//
// It imports nothing from the database, sqlc or HTTP. Use cases depend on this
// package; the postgres package implements Repository. Dependencies point inward:
//
//	http → usecase → domain ← postgres
package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Errors the rest of the app can check with errors.Is.
var (
	ErrNotFound      = errors.New("users: not found")
	ErrEmailTaken    = errors.New("users: email already registered")
	ErrUsernameTaken = errors.New("users: username already taken")
	ErrInvalid       = errors.New("users: invalid value")
)

// User is an account.
type User struct {
	ID               uuid.UUID
	Email            string
	Username         string
	PasswordHash     *string // nil for Google-only accounts
	DisplayName      string
	Locale           string
	Timezone         string
	IsAdultConfirmed bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// NewUser holds what is needed to create an account.
type NewUser struct {
	Email            string
	Username         string
	PasswordHash     *string
	DisplayName      string
	Locale           string
	Timezone         string
	IsAdultConfirmed bool
}

// Repository stores and finds accounts. Emails and usernames match in any
// letter case.
type Repository interface {
	// Create returns ErrEmailTaken or ErrUsernameTaken for a duplicate in any
	// letter case, and ErrInvalid when a value breaks a storage rule.
	Create(ctx context.Context, u NewUser) (User, error)
	// The Get methods return ErrNotFound when no account matches.
	GetByID(ctx context.Context, id uuid.UUID) (User, error)
	GetByEmail(ctx context.Context, email string) (User, error)
	GetByUsername(ctx context.Context, username string) (User, error)
}
