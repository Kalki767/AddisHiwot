package api_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kalki767/AddisHiwot/backend/internal/api"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/argon2id"
	authhttp "github.com/Kalki767/AddisHiwot/backend/internal/auth/http"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/jwttoken"
	authpostgres "github.com/Kalki767/AddisHiwot/backend/internal/auth/postgres"
	authusecase "github.com/Kalki767/AddisHiwot/backend/internal/auth/usecase"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/health"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/testdb"
	usershttp "github.com/Kalki767/AddisHiwot/backend/internal/users/http"
	userspostgres "github.com/Kalki767/AddisHiwot/backend/internal/users/postgres"
	usersusecase "github.com/Kalki767/AddisHiwot/backend/internal/users/usecase"
)

// testClock is a clock the test can move forward.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Everything real (router, use cases, Argon2id, JWT, PostgreSQL) except a
// cheaper hash cost and a movable clock.
func newServer(t *testing.T) (http.Handler, *pgxpool.Pool, *testClock) {
	t.Helper()
	pool := testdb.New(t)
	clock := &testClock{now: time.Now()}
	tokens, err := jwttoken.New([]byte("end-to-end-test-secret-at-least-32-bytes"), 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	usersRepo := userspostgres.NewRepository(pool)
	svc, err := authusecase.NewService(authusecase.Deps{
		Users:      usersRepo,
		Sessions:   authpostgres.NewSessions(pool),
		Hasher:     argon2id.New(argon2id.Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}),
		Tokens:     tokens,
		Now:        clock.Now,
		RefreshTTL: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.DiscardHandler)
	auth := authhttp.New(svc, logger)
	return api.NewRouter(api.Deps{
		Logger: logger,
		Health: health.New(pool, time.Second, logger),
		Features: []api.Registrar{
			auth,
			usershttp.New(usersusecase.NewService(usersRepo), logger, auth.RequireAuth),
		},
	}), pool, clock
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
	h, pool, _ := newServer(t)
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

type tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func tokensFrom(t *testing.T, rec *httptest.ResponseRecorder) tokens {
	t.Helper()
	var body struct {
		Tokens tokens `json:"tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Tokens.AccessToken == "" {
		t.Fatalf("no tokens in %d %s", rec.Code, rec.Body)
	}
	return body.Tokens
}

func me(h http.Handler, accessToken string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func refresh(h http.Handler, refreshToken string) *httptest.ResponseRecorder {
	return post(h, "/v1/auth/refresh", `{"refresh_token":"`+refreshToken+`"}`)
}

func code(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return env.Error.Code
}

func TestSessionsEndToEnd(t *testing.T) {
	h, pool, clock := newServer(t)
	ctx := context.Background()
	if rec := post(h, "/v1/auth/signup", signupJSON("KidistA", "kidist@example.com")); rec.Code != http.StatusAccepted {
		t.Fatalf("signup: %d %s", rec.Code, rec.Body)
	}
	loginBody := `{"identifier":"kidista","password":"correct horse battery"}`

	// Logged in: the access token opens /v1/me; no token does not.
	first := tokensFrom(t, post(h, "/v1/auth/login", loginBody))
	if rec := me(h, first.AccessToken); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"username":"KidistA"`) {
		t.Fatalf("/v1/me: %d %s", rec.Code, rec.Body)
	}
	if rec := me(h, ""); rec.Code != http.StatusUnauthorized || code(t, rec) != "UNAUTHENTICATED" {
		t.Errorf("/v1/me without a token: %d %s", rec.Code, rec.Body)
	}

	// Rotation: the refresh token is exchanged for a new pair.
	second := tokensFrom(t, refresh(h, first.RefreshToken))
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}
	if rec := me(h, second.AccessToken); rec.Code != http.StatusOK {
		t.Errorf("new access token: %d", rec.Code)
	}

	// Reuse: someone presents the old refresh token. The whole family dies.
	if rec := refresh(h, first.RefreshToken); rec.Code != http.StatusUnauthorized || code(t, rec) != "INVALID_REFRESH_TOKEN" {
		t.Fatalf("reused token: %d %s", rec.Code, rec.Body)
	}
	if rec := refresh(h, second.RefreshToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("newest token after reuse: %d, want 401 (family revoked)", rec.Code)
	}
	var reason string
	if err := pool.QueryRow(ctx, `SELECT revoked_reason FROM auth_sessions`).Scan(&reason); err != nil || reason != "reuse_detected" {
		t.Errorf("revoked_reason = %q, %v", reason, err)
	}
	var stored int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM refresh_tokens WHERE token_hash = convert_to($1, 'UTF8')`, first.RefreshToken).Scan(&stored)
	if stored != 0 {
		t.Error("a refresh token was stored in plain text")
	}

	// Logout ends a fresh session.
	third := tokensFrom(t, post(h, "/v1/auth/login", loginBody))
	if rec := post(h, "/v1/auth/logout", `{"refresh_token":"`+third.RefreshToken+`"}`); rec.Code != http.StatusNoContent {
		t.Errorf("logout: %d %s", rec.Code, rec.Body)
	}
	if rec := refresh(h, third.RefreshToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout: %d", rec.Code)
	}

	// Plan check: an expired access token gives 401 TOKEN_EXPIRED.
	fourth := tokensFrom(t, post(h, "/v1/auth/login", loginBody))
	clock.Advance(16 * time.Minute)
	if rec := me(h, fourth.AccessToken); rec.Code != http.StatusUnauthorized || code(t, rec) != "TOKEN_EXPIRED" {
		t.Errorf("expired access token: %d %s", rec.Code, rec.Body)
	}
	// ...and the refresh token still works, so the app can recover silently.
	if rec := refresh(h, fourth.RefreshToken); rec.Code != http.StatusOK {
		t.Errorf("refresh after access expiry: %d %s", rec.Code, rec.Body)
	}
}
