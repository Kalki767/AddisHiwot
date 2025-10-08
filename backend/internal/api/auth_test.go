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
	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
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

// mailbox stands in for email: it keeps the codes that would have been sent.
type mailbox struct {
	mu         sync.Mutex
	codes      map[string]string // email → last verification code
	resetCodes map[string]string // email → last password-reset code
}

func (m *mailbox) VerificationCode(_ context.Context, to, _, _, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes[strings.ToLower(to)] = code
	return nil
}

func (m *mailbox) PasswordResetCode(_ context.Context, to, _, _, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resetCodes[strings.ToLower(to)] = code
	return nil
}

func (m *mailbox) SignupAttempt(context.Context, string, string, string) error { return nil }

func (m *mailbox) code(email string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.codes[strings.ToLower(email)]
}

type allowAll struct{}

func (allowAll) Allow(string) (bool, time.Duration) { return true, 0 }

var testMailbox *mailbox // set by newServer for the test that runs it

// fakeGoogle stands in for Google's ID-token check; any token it does not know
// fails like a forged or wrong-audience token.
type fakeGoogle struct{}

func (fakeGoogle) Verify(_ context.Context, token string) (authdomain.GoogleIdentity, error) {
	switch token {
	case "token-for-abebe":
		return authdomain.GoogleIdentity{Subject: "g-abebe", Email: "abebe@gmail.com", EmailVerified: true, Name: "Abebe"}, nil
	case "token-for-kidist":
		return authdomain.GoogleIdentity{Subject: "g-kidist", Email: "kidist@gmail.com", EmailVerified: true, Name: "Kidist"}, nil
	}
	return authdomain.GoogleIdentity{}, authdomain.ErrInvalidGoogleToken
}

// Everything real (router, use cases, Argon2id, JWT, PostgreSQL) except a
// cheaper hash cost, a movable clock, no rate limits and a mailbox instead of SMTP.
func newServer(t *testing.T) (http.Handler, *pgxpool.Pool, *testClock) {
	t.Helper()
	pool := testdb.New(t)
	clock := &testClock{now: time.Now()}
	tokens, err := jwttoken.New([]byte("end-to-end-test-secret-at-least-32-bytes"), 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	usersRepo := userspostgres.NewRepository(pool)
	logger := slog.New(slog.DiscardHandler)
	testMailbox = &mailbox{codes: map[string]string{}, resetCodes: map[string]string{}}
	svc, err := authusecase.NewService(authusecase.Deps{
		Users:       usersRepo,
		Sessions:    authpostgres.NewSessions(pool),
		Codes:       authpostgres.NewCodes(pool),
		ResetTokens: authpostgres.NewResetTokens(pool),
		Identities:  authpostgres.NewIdentities(pool),
		Google:      fakeGoogle{},
		Hasher:      argon2id.New(argon2id.Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}),
		Tokens:      tokens,
		Notifier:    testMailbox,
		CodeSecret:  []byte("end-to-end-code-secret-32-bytes-long"),
		Now:         clock.Now,
		RefreshTTL:  30 * 24 * time.Hour,
		Logger:      logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := authhttp.New(svc, logger, authhttp.Limits{PerIP: allowAll{}, PerAccount: allowAll{}})
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

	// A lost response: the app retries moments later with the token it still has.
	// It gets a fresh pair; the undelivered one is dead.
	retried := tokensFrom(t, refresh(h, second.RefreshToken))
	lost := tokensFrom(t, refresh(h, retried.RefreshToken)) // pretend this answer is lost
	clock.Advance(5 * time.Second)
	recovered := tokensFrom(t, refresh(h, retried.RefreshToken))
	if rec := refresh(h, lost.RefreshToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("undelivered token after the retry: %d, want 401", rec.Code)
	}
	if rec := me(h, recovered.AccessToken); rec.Code != http.StatusOK {
		t.Errorf("after a lost-response retry: %d", rec.Code)
	}

	// Reuse: later, someone presents an old refresh token. The whole family dies.
	clock.Advance(authdomain.RefreshReuseGrace + time.Second)
	if rec := refresh(h, first.RefreshToken); rec.Code != http.StatusUnauthorized || code(t, rec) != "INVALID_REFRESH_TOKEN" {
		t.Fatalf("reused token: %d %s", rec.Code, rec.Body)
	}
	if rec := refresh(h, recovered.RefreshToken); rec.Code != http.StatusUnauthorized {
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

// Plan check: the verify flow end to end, through PostgreSQL.
func TestEmailVerificationEndToEnd(t *testing.T) {
	h, _, clock := newServer(t)
	if rec := post(h, "/v1/auth/signup", signupJSON("KidistA", "kidist@example.com")); rec.Code != http.StatusAccepted {
		t.Fatalf("signup: %d %s", rec.Code, rec.Body)
	}
	login := tokensFrom(t, post(h, "/v1/auth/login", `{"identifier":"kidista","password":"correct horse battery"}`))
	if rec := me(h, login.AccessToken); !strings.Contains(rec.Body.String(), `"email_verified":false`) {
		t.Fatalf("before verifying: %s", rec.Body)
	}

	code := testMailbox.code("kidist@example.com")
	if len(code) != 5 {
		t.Fatalf("no 5-digit code was sent: %q", code)
	}
	wrong := post(h, "/v1/auth/verify-email", `{"email":"kidist@example.com","code":"`+otherCode(code)+`"}`)
	unknown := post(h, "/v1/auth/verify-email", `{"email":"nobody@example.com","code":"12345"}`)
	if wrong.Code != http.StatusBadRequest || withoutRequestID(t, wrong.Body.String()) != withoutRequestID(t, unknown.Body.String()) {
		t.Errorf("a wrong code and an unknown email must look the same:\n%s\n%s", wrong.Body, unknown.Body)
	}

	verified := post(h, "/v1/auth/verify-email", `{"email":"KIDIST@example.com","code":"`+code+`"}`)
	if verified.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", verified.Code, verified.Body)
	}
	if rec := me(h, tokensFrom(t, verified).AccessToken); !strings.Contains(rec.Body.String(), `"email_verified":true`) {
		t.Errorf("after verifying: %s", rec.Body)
	}

	// Resend answers like sign-up, for a known address and for nobody.
	clock.Advance(time.Minute)
	a := post(h, "/v1/auth/resend-code", `{"email":"kidist@example.com"}`)
	b := post(h, "/v1/auth/resend-code", `{"email":"nobody@example.com"}`)
	if a.Code != http.StatusAccepted || a.Body.String() != b.Body.String() {
		t.Errorf("resend answers differ: %d %s / %d %s", a.Code, a.Body, b.Code, b.Body)
	}
}

func otherCode(code string) string {
	if code == "00000" {
		return "11111"
	}
	return "00000"
}

func resetCode(t *testing.T, email string) string {
	t.Helper()
	testMailbox.mu.Lock()
	defer testMailbox.mu.Unlock()
	return testMailbox.resetCodes[strings.ToLower(email)]
}

// Plan check: the whole reset flow against real PostgreSQL.
func TestPasswordResetEndToEnd(t *testing.T) {
	h, pool, clock := newServer(t)
	ctx := context.Background()
	const email = "kidist@example.com"
	if rec := post(h, "/v1/auth/signup", signupJSON("KidistA", email)); rec.Code != http.StatusAccepted {
		t.Fatalf("signup: %d %s", rec.Code, rec.Body)
	}
	old := tokensFrom(t, post(h, "/v1/auth/login", `{"identifier":"kidista","password":"correct horse battery"}`))

	// An unknown email must answer exactly like a known one.
	known := post(h, "/v1/auth/password-reset/request", `{"email":"`+email+`"}`)
	unknown := post(h, "/v1/auth/password-reset/request", `{"email":"nobody@example.com"}`)
	if known.Code != http.StatusAccepted || known.Body.String() != unknown.Body.String() {
		t.Fatalf("request answers differ: %d %s / %d %s", known.Code, known.Body, unknown.Code, unknown.Body)
	}
	code := resetCode(t, email)
	if len(code) != 5 {
		t.Fatalf("no reset code was sent: %q", code)
	}

	// Five wrong codes use the live code up, then the right one no longer works.
	wrong := otherCode(code)
	for range 5 {
		if rec := post(h, "/v1/auth/password-reset/verify", `{"email":"`+email+`","code":"`+wrong+`"}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("wrong code: %d %s", rec.Code, rec.Body)
		}
	}
	if rec := post(h, "/v1/auth/password-reset/verify", `{"email":"`+email+`","code":"`+code+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("right code after 5 wrong ones: %d, want 400", rec.Code)
	}

	// A new code, this time entered correctly.
	if rec := post(h, "/v1/auth/password-reset/request", `{"email":"`+email+`"}`); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body)
	}
	verified := post(h, "/v1/auth/password-reset/verify", `{"email":"KIDIST@example.com","code":"`+resetCode(t, email)+`"}`)
	if verified.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", verified.Code, verified.Body)
	}
	var grant struct {
		ResetToken string `json:"reset_token"`
	}
	if err := json.Unmarshal(verified.Body.Bytes(), &grant); err != nil || grant.ResetToken == "" {
		t.Fatalf("no reset token in %s", verified.Body)
	}
	body := func(password string) string {
		return `{"reset_token":"` + grant.ResetToken + `","password":"` + password + `"}`
	}

	// The same password as before is refused, and the token survives that.
	same := post(h, "/v1/auth/password-reset/complete", body("correct horse battery"))
	if same.Code != http.StatusUnprocessableEntity || !strings.Contains(same.Body.String(), "same_as_current") {
		t.Errorf("same password: %d %s", same.Code, same.Body)
	}
	if rec := post(h, "/v1/auth/password-reset/complete", body("a brand new passphrase")); rec.Code != http.StatusNoContent {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body)
	}
	// The token works once.
	if rec := post(h, "/v1/auth/password-reset/complete", body("yet another passphrase")); rec.Code != http.StatusBadRequest {
		t.Errorf("reusing the reset token: %d, want 400", rec.Code)
	}

	// Old sessions are gone, the new password works, the old one does not.
	if rec := refresh(h, old.RefreshToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("session from before the reset: %d, want 401", rec.Code)
	}
	if rec := me(h, old.AccessToken); rec.Code != http.StatusOK {
		t.Logf("note: the old access token still works until it expires (%d)", rec.Code)
	}
	if rec := post(h, "/v1/auth/login", `{"identifier":"kidista","password":"correct horse battery"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("old password: %d, want 401", rec.Code)
	}
	fresh := post(h, "/v1/auth/login", `{"identifier":"kidista","password":"a brand new passphrase"}`)
	if fresh.Code != http.StatusOK {
		t.Fatalf("new password: %d %s", fresh.Code, fresh.Body)
	}
	if rec := me(h, tokensFrom(t, fresh).AccessToken); !strings.Contains(rec.Body.String(), `"email_verified":true`) {
		t.Errorf("a completed reset should also verify the email: %s", rec.Body)
	}

	var reason string
	if err := pool.QueryRow(ctx, `SELECT revoked_reason FROM auth_sessions WHERE revoked_reason = 'password_reset' LIMIT 1`).Scan(&reason); err != nil {
		t.Errorf("no session revoked with reason password_reset: %v", err)
	}
	clock.Advance(time.Minute)
}
