// Package usecase holds what can be done with an account. It depends only on
// the users domain.
package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

// Service reads and (from Day 23) updates accounts.
type Service struct {
	users domain.Repository
}

// NewService returns the users use cases.
func NewService(users domain.Repository) *Service { return &Service{users: users} }

// Me returns the account of the logged-in user, or domain.ErrNotFound if it was
// deleted while the access token was still valid.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (domain.User, error) {
	return s.users.GetByID(ctx, userID)
}
