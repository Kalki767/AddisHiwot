package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

type oneUser struct{ u domain.User }

func (o oneUser) Create(context.Context, domain.NewUser) (domain.User, error) { return o.u, nil }
func (o oneUser) GetByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	if id != o.u.ID {
		return domain.User{}, domain.ErrNotFound
	}
	return o.u, nil
}
func (o oneUser) GetByEmail(context.Context, string) (domain.User, error)       { return o.u, nil }
func (o oneUser) GetByUsername(context.Context, string) (domain.User, error)    { return o.u, nil }
func (o oneUser) MarkEmailVerified(context.Context, uuid.UUID, time.Time) error { return nil }

func TestMe(t *testing.T) {
	u := domain.User{ID: uuid.New(), Username: "kidista"}
	svc := NewService(oneUser{u})
	if got, err := svc.Me(context.Background(), u.ID); err != nil || got.Username != "kidista" {
		t.Errorf("Me = %+v, %v", got, err)
	}
	if _, err := svc.Me(context.Background(), uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted account: err = %v, want ErrNotFound", err)
	}
}
