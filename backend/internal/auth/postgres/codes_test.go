package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/postgres"
)

var secret = []byte("code-secret-that-is-32-bytes-long!")

func TestIssueKeepsOnlyTheLatestCodeLive(t *testing.T) {
	_, pool, userID := setup(t)
	codes := postgres.NewCodes(pool)
	ctx := context.Background()
	now := time.Now()
	p := domain.PurposeVerifyEmail

	if err := codes.Issue(ctx, userID, p, domain.HashCode(secret, p, userID, "11111"), now, now.Add(domain.CodeTTL)); err != nil {
		t.Fatal(err)
	}
	if err := codes.Issue(ctx, userID, p, domain.HashCode(secret, p, userID, "22222"), now, now.Add(domain.CodeTTL)); err != nil {
		t.Fatal(err)
	}
	a, err := codes.UseAttempt(ctx, userID, p, now, domain.MaxCodeAttempts)
	if err != nil {
		t.Fatal(err)
	}
	if !domain.CodeMatches(secret, p, userID, "22222", a.Hash) || domain.CodeMatches(secret, p, userID, "11111", a.Hash) {
		t.Error("only the newest code may be live")
	}
	if n, _ := codes.CountSince(ctx, userID, p, now.Add(-time.Hour)); n != 2 {
		t.Errorf("CountSince = %d, want 2", n)
	}
	if n, _ := codes.CountSince(ctx, userID, domain.PurposeResetPassword, now.Add(-time.Hour)); n != 0 {
		t.Errorf("other purpose counted: %d", n)
	}
}

func TestAttemptsRunOutAndCodesExpire(t *testing.T) {
	_, pool, userID := setup(t)
	codes := postgres.NewCodes(pool)
	ctx := context.Background()
	now := time.Now()
	p := domain.PurposeVerifyEmail
	_ = codes.Issue(ctx, userID, p, domain.HashCode(secret, p, userID, "12345"), now, now.Add(domain.CodeTTL))

	if _, err := codes.UseAttempt(ctx, userID, p, now.Add(domain.CodeTTL), domain.MaxCodeAttempts); !errors.Is(err, domain.ErrNoLiveCode) {
		t.Errorf("at expiry: err = %v, want ErrNoLiveCode", err)
	}
	for i := range domain.MaxCodeAttempts {
		if _, err := codes.UseAttempt(ctx, userID, p, now, domain.MaxCodeAttempts); err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if _, err := codes.UseAttempt(ctx, userID, p, now, domain.MaxCodeAttempts); !errors.Is(err, domain.ErrNoLiveCode) {
		t.Errorf("attempt 6: err = %v, want ErrNoLiveCode", err)
	}
}

// Parallel guesses must not get more than MaxCodeAttempts tries between them.
func TestParallelGuessesShareTheAttemptLimit(t *testing.T) {
	_, pool, userID := setup(t)
	codes := postgres.NewCodes(pool)
	ctx := context.Background()
	now := time.Now()
	p := domain.PurposeVerifyEmail
	_ = codes.Issue(ctx, userID, p, domain.HashCode(secret, p, userID, "12345"), now, now.Add(domain.CodeTTL))

	var wg sync.WaitGroup
	var mu sync.Mutex
	got := 0
	for range 20 {
		wg.Go(func() {
			if _, err := codes.UseAttempt(ctx, userID, p, now, domain.MaxCodeAttempts); err == nil {
				mu.Lock()
				got++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if got != domain.MaxCodeAttempts {
		t.Errorf("%d of 20 parallel guesses got an attempt, want %d", got, domain.MaxCodeAttempts)
	}
}

func TestConsumeOnlyOnce(t *testing.T) {
	_, pool, userID := setup(t)
	codes := postgres.NewCodes(pool)
	ctx := context.Background()
	now := time.Now()
	p := domain.PurposeVerifyEmail
	_ = codes.Issue(ctx, userID, p, domain.HashCode(secret, p, userID, "12345"), now, now.Add(domain.CodeTTL))
	a, _ := codes.UseAttempt(ctx, userID, p, now, domain.MaxCodeAttempts)

	if err := codes.Consume(ctx, a.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := codes.Consume(ctx, a.ID, now); !errors.Is(err, domain.ErrCodeAlreadyUsed) {
		t.Errorf("second consume: err = %v, want ErrCodeAlreadyUsed", err)
	}
	if _, err := codes.UseAttempt(ctx, userID, p, now, domain.MaxCodeAttempts); !errors.Is(err, domain.ErrNoLiveCode) {
		t.Errorf("after consume: err = %v, want ErrNoLiveCode", err)
	}
}
