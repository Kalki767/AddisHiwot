package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/postgres"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/testdb"
	usersdomain "github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
	userspostgres "github.com/Kalki767/AddisHiwot/backend/internal/users/postgres"
)

var expires = time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Microsecond)

func setup(t *testing.T) (*postgres.Sessions, *pgxpool.Pool, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	u, err := userspostgres.NewRepository(pool).Create(context.Background(), usersdomain.NewUser{
		Email: "s@example.com", Username: "sessions", DisplayName: "S", Locale: "en", Timezone: "Africa/Addis_Ababa",
	})
	if err != nil {
		t.Fatal(err)
	}
	return postgres.NewSessions(pool), pool, u.ID
}

func TestCreateAndGet(t *testing.T) {
	repo, _, userID := setup(t)
	ctx := context.Background()
	_, hash := domain.NewRefreshToken()
	sessionID, err := repo.CreateSession(ctx, userID, hash, expires)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := repo.GetRefreshToken(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if rec.SessionID != sessionID || rec.UserID != userID || !rec.ExpiresAt.Equal(expires) || rec.Used || rec.SessionRevoked {
		t.Errorf("record = %+v", rec)
	}
	if _, err := repo.GetRefreshToken(ctx, domain.HashRefreshToken("unknown")); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Errorf("unknown token: err = %v", err)
	}
}

func TestRotateMarksOldUsedAndStoresNew(t *testing.T) {
	repo, _, userID := setup(t)
	ctx := context.Background()
	_, oldHash := domain.NewRefreshToken()
	sessionID, _ := repo.CreateSession(ctx, userID, oldHash, expires)
	old, _ := repo.GetRefreshToken(ctx, oldHash)

	_, newHash := domain.NewRefreshToken()
	if err := repo.Rotate(ctx, old.ID, sessionID, newHash, expires); err != nil {
		t.Fatal(err)
	}
	if rec, _ := repo.GetRefreshToken(ctx, oldHash); !rec.Used {
		t.Error("old token not marked used")
	}
	if rec, err := repo.GetRefreshToken(ctx, newHash); err != nil || rec.Used || rec.SessionID != sessionID {
		t.Errorf("new token: %+v, %v", rec, err)
	}

	_, thirdHash := domain.NewRefreshToken()
	if err := repo.Rotate(ctx, old.ID, sessionID, thirdHash, expires); !errors.Is(err, domain.ErrRefreshTokenReused) {
		t.Errorf("rotating a used token: err = %v, want ErrRefreshTokenReused", err)
	}
	if _, err := repo.GetRefreshToken(ctx, thirdHash); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Error("a failed rotation must not store its new token")
	}
}

// Two devices (or a thief and the owner) racing with the same token: exactly one wins.
func TestConcurrentRotationHasOneWinner(t *testing.T) {
	repo, _, userID := setup(t)
	ctx := context.Background()
	_, hash := domain.NewRefreshToken()
	sessionID, _ := repo.CreateSession(ctx, userID, hash, expires)
	old, _ := repo.GetRefreshToken(ctx, hash)

	const racers = 20
	var wg sync.WaitGroup
	results := make(chan error, racers)
	for range racers {
		wg.Go(func() {
			_, newHash := domain.NewRefreshToken()
			results <- repo.Rotate(ctx, old.ID, sessionID, newHash, expires)
		})
	}
	wg.Wait()
	close(results)
	won, reused := 0, 0
	for err := range results {
		switch {
		case err == nil:
			won++
		case errors.Is(err, domain.ErrRefreshTokenReused):
			reused++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 || reused != racers-1 {
		t.Errorf("won %d, reused %d; want exactly 1 winner", won, reused)
	}
}

func TestRevokeSession(t *testing.T) {
	repo, _, userID := setup(t)
	ctx := context.Background()
	_, hash := domain.NewRefreshToken()
	sessionID, _ := repo.CreateSession(ctx, userID, hash, expires)
	for range 2 { // revoking twice is fine
		if err := repo.RevokeSession(ctx, sessionID, domain.RevokeLogout); err != nil {
			t.Fatal(err)
		}
	}
	if rec, _ := repo.GetRefreshToken(ctx, hash); !rec.SessionRevoked {
		t.Error("session not revoked")
	}
}

func TestDeletingTheUserDeletesSessions(t *testing.T) {
	repo, pool, userID := setup(t)
	ctx := context.Background()
	_, hash := domain.NewRefreshToken()
	if _, err := repo.CreateSession(ctx, userID, hash, expires); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetRefreshToken(ctx, hash); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Errorf("token survived its user: %v", err)
	}
}
