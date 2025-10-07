package authhttp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/usecase"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/validate"
	usersdomain "github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

type fakeService struct {
	outcome   usecase.SignupOutcome
	err       error
	user      usersdomain.User
	tokens    usecase.Tokens
	principal authdomain.Principal
	gotInput  usecase.SignupInput
	gotToken  string
}

func (f *fakeService) Signup(_ context.Context, in usecase.SignupInput) (usecase.SignupOutcome, error) {
	f.gotInput = in
	return f.outcome, f.err
}

func (f *fakeService) Login(context.Context, string, string) (usecase.LoginResult, error) {
	return usecase.LoginResult{User: f.user, Tokens: f.tokens}, f.err
}

func (f *fakeService) Refresh(_ context.Context, tok string) (usecase.Tokens, error) {
	f.gotToken = tok
	return f.tokens, f.err
}

func (f *fakeService) Logout(_ context.Context, tok string) error {
	f.gotToken = tok
	return f.err
}

func (f *fakeService) Authenticate(tok string) (authdomain.Principal, error) {
	f.gotToken = tok
	return f.principal, f.err
}

func (f *fakeService) VerifyEmail(_ context.Context, email, code string) (usecase.LoginResult, error) {
	f.gotToken = email + "/" + code
	return usecase.LoginResult{User: f.user, Tokens: f.tokens}, f.err
}

func (f *fakeService) ResendVerification(_ context.Context, email string) error {
	f.gotToken = email
	return f.err
}

func (f *fakeService) RequestPasswordReset(_ context.Context, email string) error {
	f.gotToken = email
	return f.err
}

func (f *fakeService) VerifyPasswordResetCode(_ context.Context, email, code string) (usecase.ResetGrant, error) {
	f.gotToken = email + "/" + code
	return usecase.ResetGrant{Token: "reset-token", ExpiresAt: someTokens.AccessExpiresAt}, f.err
}

func (f *fakeService) CompletePasswordReset(_ context.Context, token, password string) error {
	f.gotToken = token + "/" + password
	return f.err
}

// allowAll is a limiter that never limits.
type allowAll struct{}

func (allowAll) Allow(string) (bool, time.Duration) { return true, 0 }

// allowN allows n requests per key, then asks to wait.
type allowN struct {
	n    int
	seen map[string]int
}

func (a *allowN) Allow(key string) (bool, time.Duration) {
	if a.seen == nil {
		a.seen = map[string]int{}
	}
	a.seen[key]++
	return a.seen[key] <= a.n, 42 * time.Second
}

var noLimits = Limits{PerIP: allowAll{}, PerAccount: allowAll{}}

func call(t *testing.T, svc Service, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return callWith(t, svc, noLimits, path, body)
}

func callWith(t *testing.T, svc Service, limits Limits, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	New(svc, slog.New(slog.DiscardHandler), limits).Register(mux)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

const signupBody = `{"username":"kidista","email":"kidist@example.com","password":"correct horse battery","is_adult":true,"accept_terms":true,"locale":"am"}`

func TestSignupPassesTheFieldsToTheUseCase(t *testing.T) {
	svc := &fakeService{outcome: usecase.Created}
	rec := call(t, svc, "/v1/auth/signup", signupBody)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	want := usecase.SignupInput{Username: "kidista", Email: "kidist@example.com", Password: "correct horse battery",
		IsAdult: true, AcceptTerms: true, Locale: "am"}
	if svc.gotInput != want {
		t.Errorf("use case got %+v", svc.gotInput)
	}
}

// A new email and an already-registered one must get byte-identical answers.
func TestSignupAnswersTheSameForNewAndRegisteredEmails(t *testing.T) {
	created := call(t, &fakeService{outcome: usecase.Created}, "/v1/auth/signup", signupBody)
	existing := call(t, &fakeService{outcome: usecase.EmailAlreadyRegistered}, "/v1/auth/signup", signupBody)
	if created.Code != existing.Code || created.Body.String() != existing.Body.String() {
		t.Errorf("answers differ:\n new:      %d %s\n existing: %d %s", created.Code, created.Body, existing.Code, existing.Body)
	}
	if !strings.Contains(created.Body.String(), "verification_pending") {
		t.Errorf("body = %s", created.Body)
	}
}

func TestSignupValidationErrorsAre422(t *testing.T) {
	svc := &fakeService{err: validate.Errors{{Field: "is_adult", Code: "must_be_adult", Message: "x"}}}
	rec := call(t, svc, "/v1/auth/signup", signupBody)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"must_be_adult"`) {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestSignupRejectsUnknownFields(t *testing.T) {
	rec := call(t, &fakeService{}, "/v1/auth/signup", `{"username":"k","is_admin":true}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "UNKNOWN_FIELD") {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestLoginReturnsTheUserWithoutSecrets(t *testing.T) {
	hash := "$argon2id$secret"
	svc := &fakeService{user: usersdomain.User{ID: uuid.New(), Username: "kidista", Email: "kidist@example.com",
		DisplayName: "Kidist", Locale: "am", Timezone: "Africa/Addis_Ababa", PasswordHash: &hash, CreatedAt: time.Now()}}
	rec := call(t, svc, "/v1/auth/login", `{"identifier":"kidista","password":"correct horse battery"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"username":"kidista"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	for _, secret := range []string{"argon2id", "password"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("response contains %q: %s", secret, rec.Body)
		}
	}
}

func TestLoginFailureIs401(t *testing.T) {
	rec := call(t, &fakeService{err: authdomain.ErrInvalidCredentials}, "/v1/auth/login", `{"identifier":"x","password":"y"}`)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), CodeLoginFailed) {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestUnexpectedErrorsAre500(t *testing.T) {
	rec := call(t, &fakeService{err: errors.New("database is on fire")}, "/v1/auth/login", `{"identifier":"x","password":"y"}`)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "fire") {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

var someTokens = usecase.Tokens{AccessToken: "acc", AccessExpiresAt: time.Date(2026, 10, 26, 9, 15, 0, 0, time.UTC),
	RefreshToken: "ref", RefreshExpiresAt: time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)}

func TestLoginReturnsTokensUncached(t *testing.T) {
	svc := &fakeService{user: usersdomain.User{ID: uuid.New(), Username: "kidista"}, tokens: someTokens}
	rec := call(t, svc, "/v1/auth/login", `{"identifier":"kidista","password":"correct horse battery"}`)
	body := rec.Body.String()
	for _, want := range []string{`"token_type":"Bearer"`, `"access_token":"acc"`, `"refresh_token":"ref"`,
		`"access_token_expires_at":"2026-10-26T09:15:00Z"`, `"username":"kidista"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %s: %s", want, body)
		}
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("token responses must not be cached")
	}
}

func TestRefresh(t *testing.T) {
	svc := &fakeService{tokens: someTokens}
	rec := call(t, svc, "/v1/auth/refresh", `{"refresh_token":"old"}`)
	if rec.Code != http.StatusOK || svc.gotToken != "old" || !strings.Contains(rec.Body.String(), `"refresh_token":"ref"`) {
		t.Errorf("got %d %s (token %q)", rec.Code, rec.Body, svc.gotToken)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("token responses must not be cached")
	}
}

func TestRefreshFailuresAre401(t *testing.T) {
	for _, err := range []error{authdomain.ErrInvalidRefreshToken, authdomain.ErrRefreshTokenReused} {
		rec := call(t, &fakeService{err: err}, "/v1/auth/refresh", `{"refresh_token":"x"}`)
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), CodeInvalidRefreshToken) {
			t.Errorf("%v: got %d %s", err, rec.Code, rec.Body)
		}
	}
}

func TestLogoutIs204(t *testing.T) {
	svc := &fakeService{}
	rec := call(t, svc, "/v1/auth/logout", `{"refresh_token":"bye"}`)
	if rec.Code != http.StatusNoContent || svc.gotToken != "bye" || rec.Body.Len() != 0 {
		t.Errorf("got %d %q (token %q)", rec.Code, rec.Body, svc.gotToken)
	}
}

func protected(t *testing.T, svc *fakeService, authorization string) (*httptest.ResponseRecorder, *authdomain.Principal) {
	t.Helper()
	var seen *authdomain.Principal
	h := New(svc, slog.New(slog.DiscardHandler), noLimits).RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := authdomain.PrincipalFrom(r.Context())
		if ok {
			seen = &p
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, seen
}

func TestRequireAuthLetsAValidTokenThrough(t *testing.T) {
	want := authdomain.Principal{UserID: uuid.New(), SessionID: uuid.New()}
	svc := &fakeService{principal: want}
	for _, header := range []string{"Bearer good-token", "bearer good-token", "BEARER good-token"} {
		rec, seen := protected(t, svc, header)
		if rec.Code != http.StatusOK || seen == nil || *seen != want || svc.gotToken != "good-token" {
			t.Errorf("%q: got %d, principal %+v, token %q", header, rec.Code, seen, svc.gotToken)
		}
	}
}

func TestRequireAuthRejections(t *testing.T) {
	tests := []struct {
		name, header string
		err          error
		code         string
	}{
		{"no header", "", nil, CodeUnauthenticated},
		{"basic auth", "Basic a2lkaXN0OnB3", nil, CodeUnauthenticated},
		{"empty bearer", "Bearer ", nil, CodeUnauthenticated},
		{"scheme only", "Bearer", nil, CodeUnauthenticated},
		{"bearer look-alike", "Bearerx abc", nil, CodeUnauthenticated},
		{"expired", "Bearer old", authdomain.ErrTokenExpired, CodeTokenExpired},
		{"forged", "Bearer forged", authdomain.ErrInvalidToken, CodeInvalidToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, seen := protected(t, &fakeService{err: tt.err}, tt.header)
			if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"`+tt.code+`"`) {
				t.Errorf("got %d %s, want 401 %s", rec.Code, rec.Body, tt.code)
			}
			if seen != nil {
				t.Error("the handler ran without a valid token")
			}
			if !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
				t.Errorf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestVerifyEmail(t *testing.T) {
	svc := &fakeService{user: usersdomain.User{ID: uuid.New(), Username: "kidista"}, tokens: someTokens}
	rec := call(t, svc, "/v1/auth/verify-email", `{"email":"kidist@example.com","code":"04217"}`)
	if rec.Code != http.StatusOK || svc.gotToken != "kidist@example.com/04217" ||
		!strings.Contains(rec.Body.String(), `"access_token":"acc"`) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("got %d %s (input %q)", rec.Code, rec.Body, svc.gotToken)
	}
}

func TestVerifyEmailWithAWrongCode(t *testing.T) {
	rec := call(t, &fakeService{err: authdomain.ErrInvalidCode}, "/v1/auth/verify-email", `{"email":"k@example.com","code":"11111"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), CodeInvalidCode) {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

// Resend must look exactly like sign-up, whoever asks.
func TestResendCodeAnswersLikeSignup(t *testing.T) {
	resend := call(t, &fakeService{}, "/v1/auth/resend-code", `{"email":"anyone@example.com"}`)
	signup := call(t, &fakeService{outcome: usecase.Created}, "/v1/auth/signup", signupBody)
	if resend.Code != http.StatusAccepted || resend.Body.String() != signup.Body.String() {
		t.Errorf("resend %d %s vs signup %d %s", resend.Code, resend.Body, signup.Code, signup.Body)
	}
}

// Plan check: a limited request gets 429 with a Retry-After header.
func TestPerAccountLimitAnswers429(t *testing.T) {
	limits := Limits{PerIP: allowAll{}, PerAccount: &allowN{n: 2}}
	svc := &fakeService{err: authdomain.ErrInvalidCredentials}
	body := `{"identifier":"KidistA","password":"guess"}`
	for range 2 {
		if rec := callWith(t, svc, limits, "/v1/auth/login", body); rec.Code != http.StatusUnauthorized {
			t.Fatalf("allowed attempt: %d", rec.Code)
		}
	}
	// Same account in other letters: still the same limit.
	rec := callWith(t, svc, limits, "/v1/auth/login", `{"identifier":"kidista","password":"guess"}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "42" ||
		!strings.Contains(rec.Body.String(), "RATE_LIMITED") {
		t.Errorf("3rd attempt: %d %q %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body)
	}
}

func TestPerIPLimitCoversEveryAuthRoute(t *testing.T) {
	for _, path := range []string{"/v1/auth/signup", "/v1/auth/login", "/v1/auth/refresh", "/v1/auth/logout",
		"/v1/auth/verify-email", "/v1/auth/resend-code", "/v1/auth/password-reset/request",
		"/v1/auth/password-reset/verify", "/v1/auth/password-reset/complete"} {
		limits := Limits{PerIP: &allowN{n: 0}, PerAccount: allowAll{}}
		if rec := callWith(t, &fakeService{}, limits, path, `{}`); rec.Code != http.StatusTooManyRequests {
			t.Errorf("%s: %d, want 429", path, rec.Code)
		}
	}
}

func TestPasswordResetRequestAnswersTheSameForEveryone(t *testing.T) {
	known := call(t, &fakeService{}, "/v1/auth/password-reset/request", `{"email":"kidist@example.com"}`)
	unknown := call(t, &fakeService{}, "/v1/auth/password-reset/request", `{"email":"nobody@example.com"}`)
	if known.Code != http.StatusAccepted || known.Body.String() != unknown.Body.String() {
		t.Errorf("answers differ: %d %s / %d %s", known.Code, known.Body, unknown.Code, unknown.Body)
	}
}

func TestPasswordResetVerifyReturnsAToken(t *testing.T) {
	svc := &fakeService{}
	rec := call(t, svc, "/v1/auth/password-reset/verify", `{"email":"kidist@example.com","code":"04217"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"reset_token":"reset-token"`) ||
		rec.Header().Get("Cache-Control") != "no-store" || svc.gotToken != "kidist@example.com/04217" {
		t.Errorf("got %d %s (input %q, cache %q)", rec.Code, rec.Body, svc.gotToken, rec.Header().Get("Cache-Control"))
	}
}

func TestPasswordResetVerifyWithAWrongCode(t *testing.T) {
	rec := call(t, &fakeService{err: authdomain.ErrInvalidCode}, "/v1/auth/password-reset/verify", `{"email":"k@example.com","code":"11111"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), CodeInvalidCode) {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestPasswordResetComplete(t *testing.T) {
	svc := &fakeService{}
	rec := call(t, svc, "/v1/auth/password-reset/complete", `{"reset_token":"tok","password":"a brand new passphrase"}`)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 || svc.gotToken != "tok/a brand new passphrase" {
		t.Errorf("got %d %q (input %q)", rec.Code, rec.Body, svc.gotToken)
	}
}

func TestPasswordResetCompleteErrors(t *testing.T) {
	bad := call(t, &fakeService{err: authdomain.ErrInvalidResetToken}, "/v1/auth/password-reset/complete", `{"reset_token":"tok","password":"a brand new passphrase"}`)
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), CodeInvalidResetToken) {
		t.Errorf("bad token: %d %s", bad.Code, bad.Body)
	}
	weak := call(t, &fakeService{err: validate.Errors{{Field: "password", Code: "same_as_current", Message: "x"}}},
		"/v1/auth/password-reset/complete", `{"reset_token":"tok","password":"correct horse battery"}`)
	if weak.Code != http.StatusUnprocessableEntity || !strings.Contains(weak.Body.String(), "same_as_current") {
		t.Errorf("same password: %d %s", weak.Code, weak.Body)
	}
}
