package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/testdb"
	"github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/users/postgres"
)

func ptr(s string) *string { return &s }

func newUser(email, username string) domain.NewUser {
	return domain.NewUser{
		Email:            email,
		Username:         username,
		PasswordHash:     ptr("$argon2id$fake"),
		DisplayName:      "Kidist",
		Locale:           "am",
		Timezone:         "Africa/Addis_Ababa",
		IsAdultConfirmed: true,
	}
}

func TestCreateAndReadBack(t *testing.T) {
	repo := postgres.NewRepository(testdb.New(t))
	ctx := context.Background()

	created, err := repo.Create(ctx, newUser("kidist@example.com", "kidist"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID.Version() != 7 {
		t.Errorf("id version = %d, want a UUIDv7", created.ID.Version())
	}
	if created.CreatedAt.IsZero() || !created.CreatedAt.Equal(created.UpdatedAt) {
		t.Errorf("timestamps: created %v, updated %v", created.CreatedAt, created.UpdatedAt)
	}

	for name, get := range map[string]func() (domain.User, error){
		"by id":       func() (domain.User, error) { return repo.GetByID(ctx, created.ID) },
		"by email":    func() (domain.User, error) { return repo.GetByEmail(ctx, "kidist@example.com") },
		"by username": func() (domain.User, error) { return repo.GetByUsername(ctx, "kidist") },
	} {
		got, err := get()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.ID != created.ID || got.Locale != "am" || !got.IsAdultConfirmed || *got.PasswordHash != "$argon2id$fake" {
			t.Errorf("%s: got %+v", name, got)
		}
	}
}

func TestLookupsIgnoreLetterCase(t *testing.T) {
	repo := postgres.NewRepository(testdb.New(t))
	ctx := context.Background()
	created, err := repo.Create(ctx, newUser("Kidist@Example.com", "KidistA"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetByEmail(ctx, "KIDIST@example.COM"); err != nil || got.ID != created.ID {
		t.Errorf("GetByEmail in other case: %v, %v", got.ID, err)
	}
	if got, err := repo.GetByUsername(ctx, "kidista"); err != nil || got.ID != created.ID {
		t.Errorf("GetByUsername in other case: %v, %v", got.ID, err)
	}
	if created.Email != "Kidist@Example.com" {
		t.Errorf("email stored as %q, want the case the user typed", created.Email)
	}
}

func TestDuplicatesInAnyCaseAreRejected(t *testing.T) {
	repo := postgres.NewRepository(testdb.New(t))
	ctx := context.Background()
	if _, err := repo.Create(ctx, newUser("kidist@example.com", "kidist")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, newUser("KIDIST@example.com", "other")); !errors.Is(err, domain.ErrEmailTaken) {
		t.Errorf("same email, other case: err = %v, want ErrEmailTaken", err)
	}
	if _, err := repo.Create(ctx, newUser("other@example.com", "Kidist")); !errors.Is(err, domain.ErrUsernameTaken) {
		t.Errorf("same username, other case: err = %v, want ErrUsernameTaken", err)
	}
}

func TestNotFound(t *testing.T) {
	repo := postgres.NewRepository(testdb.New(t))
	ctx := context.Background()
	if _, err := repo.GetByID(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetByID: %v", err)
	}
	if _, err := repo.GetByEmail(ctx, "nobody@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetByEmail: %v", err)
	}
	if _, err := repo.GetByUsername(ctx, "nobody"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetByUsername: %v", err)
	}
}

func TestGoogleOnlyAccountHasNoPassword(t *testing.T) {
	repo := postgres.NewRepository(testdb.New(t))
	u := newUser("g@example.com", "googleuser")
	u.PasswordHash = nil
	created, err := repo.Create(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	if created.PasswordHash != nil {
		t.Errorf("PasswordHash = %q, want nil", *created.PasswordHash)
	}
}

func TestDatabaseRulesRejectBadValues(t *testing.T) {
	repo := postgres.NewRepository(testdb.New(t))
	tests := map[string]func(*domain.NewUser){
		"username too short":      func(u *domain.NewUser) { u.Username = "ab" },
		"username starts digit":   func(u *domain.NewUser) { u.Username = "1kidist" },
		"username with @":         func(u *domain.NewUser) { u.Username = "kid@ist" },
		"username too long":       func(u *domain.NewUser) { u.Username = "a23456789012345678901" },
		"email without @":         func(u *domain.NewUser) { u.Email = "kidist.example.com" },
		"empty display name":      func(u *domain.NewUser) { u.DisplayName = "" },
		"unsupported locale":      func(u *domain.NewUser) { u.Locale = "fr" },
		"display name 51 letters": func(u *domain.NewUser) { u.DisplayName = strings.Repeat("ሀ", 51) },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			u := newUser("rules@example.com", "rules")
			change(&u)
			if _, err := repo.Create(context.Background(), u); !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestUpdatedAtMovesOnUpdate(t *testing.T) {
	pool := testdb.New(t)
	repo := postgres.NewRepository(pool)
	ctx := context.Background()
	created, err := repo.Create(ctx, newUser("t@example.com", "timer"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := pool.Exec(ctx, "UPDATE users SET display_name = 'New' WHERE id = $1", created.ID); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.After(created.UpdatedAt) || !got.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("after update: created %v → %v, updated %v → %v", created.CreatedAt, got.CreatedAt, created.UpdatedAt, got.UpdatedAt)
	}
}

// Amharic letters take 3 bytes each; the length rule must count letters.
func TestDisplayNameLimitCountsLettersNotBytes(t *testing.T) {
	repo := postgres.NewRepository(testdb.New(t))
	u := newUser("am@example.com", "amharic")
	u.DisplayName = strings.Repeat("ሀ", 50) // 150 bytes
	if _, err := repo.Create(context.Background(), u); err != nil {
		t.Errorf("50 Amharic letters rejected: %v", err)
	}
}
