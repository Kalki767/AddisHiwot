package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/postgres"
	usersdomain "github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
	userspostgres "github.com/Kalki767/AddisHiwot/backend/internal/users/postgres"
)

func TestLinkGoogleThenFind(t *testing.T) {
	_, pool, userID := setup(t)
	ids := postgres.NewIdentities(pool)
	ctx := context.Background()
	if _, err := ids.FindGoogleUser(ctx, "google-sub-1"); !errors.Is(err, domain.ErrNoIdentity) {
		t.Fatalf("before linking: err = %v, want ErrNoIdentity", err)
	}
	link := domain.GoogleLink{UserID: userID, Subject: "google-sub-1", Now: time.Now()}
	if err := ids.LinkGoogle(ctx, link); err != nil {
		t.Fatal(err)
	}
	// Linking the same pair again (two taps at once) is fine.
	if err := ids.LinkGoogle(ctx, link); err != nil {
		t.Errorf("linking again: %v", err)
	}
	got, err := ids.FindGoogleUser(ctx, "google-sub-1")
	if err != nil || got != userID {
		t.Errorf("FindGoogleUser = %v, %v; want %v", got, err, userID)
	}
	var verified *time.Time
	_ = pool.QueryRow(ctx, "SELECT email_verified_at FROM users WHERE id = $1", userID).Scan(&verified)
	if verified == nil {
		t.Error("linking Google must mark the email verified")
	}
}

func TestLinkGoogleConflicts(t *testing.T) {
	_, pool, userID := setup(t)
	ids := postgres.NewIdentities(pool)
	ctx := context.Background()
	other, err := userspostgres.NewRepository(pool).Create(ctx, usersdomain.NewUser{
		Email: "o@example.com", Username: "other", DisplayName: "O", Locale: "en", Timezone: "Africa/Addis_Ababa",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := ids.LinkGoogle(ctx, domain.GoogleLink{UserID: userID, Subject: "sub-a", Now: now}); err != nil {
		t.Fatal(err)
	}
	for name, l := range map[string]domain.GoogleLink{
		"Google account linked to another account": {UserID: other.ID, Subject: "sub-a", Now: now},
		"account already linked to another Google": {UserID: userID, Subject: "sub-b", Now: now},
	} {
		if err := ids.LinkGoogle(ctx, l); !errors.Is(err, domain.ErrIdentityConflict) {
			t.Errorf("%s: err = %v, want ErrIdentityConflict", name, err)
		}
	}
}

// Linking to an account whose email was never verified drops the password and
// ends its sessions, in the same transaction.
func TestLinkGoogleCanDropThePassword(t *testing.T) {
	sessions, pool, userID := setup(t)
	ids := postgres.NewIdentities(pool)
	ctx := context.Background()
	_, _ = pool.Exec(ctx, "UPDATE users SET password_hash = 'set-by-someone' WHERE id = $1", userID)
	_, refresh := domain.NewRefreshToken()
	if _, err := sessions.CreateSession(ctx, userID, refresh, expires); err != nil {
		t.Fatal(err)
	}

	if err := ids.LinkGoogle(ctx, domain.GoogleLink{UserID: userID, Subject: "sub", DropPassword: true, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var pw *string
	_ = pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1", userID).Scan(&pw)
	if pw != nil {
		t.Errorf("password_hash = %q, want NULL", *pw)
	}
	rec, err := sessions.GetRefreshToken(ctx, refresh)
	if err != nil || !rec.SessionRevoked {
		t.Errorf("session not revoked: %+v %v", rec, err)
	}
}

// Parallel first sign-ins with one Google account end with one link.
func TestConcurrentLinksOfTheSamePair(t *testing.T) {
	_, pool, userID := setup(t)
	ids := postgres.NewIdentities(pool)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Go(func() {
			errs <- ids.LinkGoogle(context.Background(), domain.GoogleLink{UserID: userID, Subject: "sub", Now: time.Now()})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("LinkGoogle: %v", err)
		}
	}
}
