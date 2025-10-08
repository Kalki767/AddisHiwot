package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/postgres"
)

func TestResetTokenLifecycle(t *testing.T) {
	_, pool, userID := setup(t)
	tokens := postgres.NewResetTokens(pool)
	ctx := context.Background()
	now := time.Now()
	_, hash := domain.NewResetToken()
	if err := tokens.Create(ctx, userID, hash, now, now.Add(domain.ResetTokenTTL)); err != nil {
		t.Fatal(err)
	}

	found, err := tokens.Find(ctx, hash, now)
	if err != nil || found.UserID != userID {
		t.Fatalf("Find = %+v, %v", found, err)
	}
	if _, err := tokens.Find(ctx, hash, now.Add(domain.ResetTokenTTL)); !errors.Is(err, domain.ErrInvalidResetToken) {
		t.Errorf("at expiry: err = %v, want ErrInvalidResetToken", err)
	}
	if _, err := tokens.Find(ctx, domain.HashResetToken("never issued"), now); !errors.Is(err, domain.ErrInvalidResetToken) {
		t.Errorf("unknown token: err = %v", err)
	}

	if err := tokens.Complete(ctx, found, "new-hash", now); err != nil {
		t.Fatal(err)
	}
	if _, err := tokens.Find(ctx, hash, now); !errors.Is(err, domain.ErrInvalidResetToken) {
		t.Errorf("after complete: err = %v", err)
	}
}

// Complete sets the password, ends every session and verifies the email; a
// token spent by another request changes nothing at all.
func TestCompleteResetIsAllOrNothing(t *testing.T) {
	sessions, pool, userID := setup(t)
	tokens := postgres.NewResetTokens(pool)
	ctx := context.Background()
	now := time.Now()
	refresh := make([][]byte, 3)
	for i := range refresh {
		_, refresh[i] = domain.NewRefreshToken()
		if _, err := sessions.CreateSession(ctx, userID, refresh[i], expires); err != nil {
			t.Fatal(err)
		}
	}
	_, hash := domain.NewResetToken()
	_ = tokens.Create(ctx, userID, hash, now, now.Add(domain.ResetTokenTTL))
	rt, err := tokens.Find(ctx, hash, now)
	if err != nil {
		t.Fatal(err)
	}

	if err := tokens.Complete(ctx, rt, "first-hash", now); err != nil {
		t.Fatal(err)
	}
	var pw string
	var verified *time.Time
	if err := pool.QueryRow(ctx, "SELECT password_hash, email_verified_at FROM users WHERE id = $1", userID).Scan(&pw, &verified); err != nil {
		t.Fatal(err)
	}
	if pw != "first-hash" || verified == nil {
		t.Errorf("password_hash = %q, email_verified_at = %v", pw, verified)
	}
	for i, h := range refresh {
		if rec, err := sessions.GetRefreshToken(ctx, h); err != nil || !rec.SessionRevoked {
			t.Errorf("session %d not revoked: %+v %v", i, rec, err)
		}
	}

	// A session started after the reset must survive a replayed token.
	_, later := domain.NewRefreshToken()
	if _, err := sessions.CreateSession(ctx, userID, later, expires); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Complete(ctx, rt, "second-hash", now); !errors.Is(err, domain.ErrInvalidResetToken) {
		t.Fatalf("second complete: err = %v, want ErrInvalidResetToken", err)
	}
	_ = pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1", userID).Scan(&pw)
	if pw != "first-hash" {
		t.Errorf("a spent token changed the password to %q", pw)
	}
	if rec, err := sessions.GetRefreshToken(ctx, later); err != nil || rec.SessionRevoked {
		t.Errorf("a spent token revoked a new session: %+v %v", rec, err)
	}
}
