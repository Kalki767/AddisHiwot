package api_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kalki767/AddisHiwot/backend/internal/api"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/argon2id"
	authhttp "github.com/Kalki767/AddisHiwot/backend/internal/auth/http"
	authusecase "github.com/Kalki767/AddisHiwot/backend/internal/auth/usecase"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/health"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/testdb"
	userspostgres "github.com/Kalki767/AddisHiwot/backend/internal/users/postgres"
)

// Everything real (router, use case, Argon2id, PostgreSQL) except a cheaper hash cost.
func newServer(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	hasher := argon2id.New(argon2id.Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	svc, err := authusecase.NewService(userspostgres.NewRepository(pool), hasher, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.DiscardHandler)
	return api.NewRouter(api.Deps{
		Logger:   logger,
		Health:   health.New(pool, time.Second, logger),
		Features: []api.Registrar{authhttp.New(svc, logger)},
	}), pool
}

func post(h http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func signupJSON(username, email string) string {
	return `{"username":"` + username + `","email":"` + email + `","password":"correct horse battery","is_adult":true,"accept_terms":true}`
}

// withoutRequestID removes the per-request ID so two error bodies can be compared.
func withoutRequestID(t *testing.T, body string) string {
	t.Helper()
	var env map[string]map[string]any
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("not JSON: %s", body)
	}
	delete(env["error"], "request_id")
	b, _ := json.Marshal(env)
	return string(b)
}

func TestSignupAndLoginEndToEnd(t *testing.T) {
	h, pool := newServer(t)
	ctx := context.Background()

	first := post(h, "/v1/auth/signup", signupJSON("KidistA", "kidist@example.com"))
	if first.Code != http.StatusAccepted {
		t.Fatalf("signup: %d %s", first.Code, first.Body)
	}

	var hash string
	var adultAt, termsAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT password_hash, adult_confirmed_at, terms_accepted_at FROM users`).Scan(&hash, &adultAt, &termsAt); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, "correct horse") {
		t.Errorf("stored password_hash = %q, want an Argon2id hash", hash)
	}
	if adultAt == nil || termsAt == nil {
		t.Error("18+ confirmation and terms acceptance must be stored with their time")
	}

	// Same email in other letters: same answer, no second account.
	again := post(h, "/v1/auth/signup", signupJSON("someoneelse", "KIDIST@example.com"))
	if again.Code != first.Code || again.Body.String() != first.Body.String() {
		t.Errorf("duplicate email answered differently:\n%d %s\n%d %s", first.Code, first.Body, again.Code, again.Body)
	}
	var count int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count)
	if count != 1 {
		t.Errorf("accounts = %d, want 1", count)
	}

	taken := post(h, "/v1/auth/signup", signupJSON("kidista", "other@example.com"))
	if taken.Code != http.StatusUnprocessableEntity || !strings.Contains(taken.Body.String(), "username_taken") {
		t.Errorf("taken username: %d %s", taken.Code, taken.Body)
	}

	for _, id := range []string{"kidist@example.com", "KidistA", "kidista"} {
		ok := post(h, "/v1/auth/login", `{"identifier":"`+id+`","password":"correct horse battery"}`)
		if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), `"username":"KidistA"`) {
			t.Errorf("login as %q: %d %s", id, ok.Code, ok.Body)
		}
	}

	wrongPassword := post(h, "/v1/auth/login", `{"identifier":"kidista","password":"wrong horse battery"}`)
	unknown := post(h, "/v1/auth/login", `{"identifier":"nobody@example.com","password":"wrong horse battery"}`)
	if wrongPassword.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized {
		t.Fatalf("codes: wrong password %d, unknown %d", wrongPassword.Code, unknown.Code)
	}
	if withoutRequestID(t, wrongPassword.Body.String()) != withoutRequestID(t, unknown.Body.String()) {
		t.Errorf("a wrong password and an unknown account must look the same:\n%s\n%s", wrongPassword.Body, unknown.Body)
	}
}
