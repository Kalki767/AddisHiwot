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
	sessions, pool, userID := setup(t)
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

	if err := tokens.Consume(ctx, found.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Consume(ctx, found.ID, now); !errors.Is(err, domain.ErrInvalidResetToken) {
		t.Errorf("second consume: err = %v, want ErrInvalidResetToken", err)
	}
	if _, err := tokens.Find(ctx, hash, now); !errors.Is(err, domain.ErrInvalidResetToken) {
		t.Errorf("after consume: err = %v", err)
	}
	_ = sessions
}

func TestRevokeUserSessionsEndsThemAll(t *testing.T) {
	sessions, _, userID := setup(t)
	ctx := context.Background()
	hashes := make([][]byte, 3)
	for i := range hashes {
		_, h := domain.NewRefreshToken()
		hashes[i] = h
		if _, err := sessions.CreateSession(ctx, userID, h, expires); err != nil {
			t.Fatal(err)
		}
	}
	if err := sessions.RevokeUserSessions(ctx, userID, domain.RevokePasswordReset); err != nil {
		t.Fatal(err)
	}
	for i, h := range hashes {
		rec, err := sessions.GetRefreshToken(ctx, h)
		if err != nil || !rec.SessionRevoked {
			t.Errorf("session %d not revoked: %+v %v", i, rec, err)
		}
	}
	// Revoking again changes nothing and is not an error.
	if err := sessions.RevokeUserSessions(ctx, userID, domain.RevokePasswordReset); err != nil {
		t.Errorf("second call: %v", err)
	}
}
