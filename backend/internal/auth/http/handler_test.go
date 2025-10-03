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
	outcome  usecase.SignupOutcome
	err      error
	user     usersdomain.User
	gotInput usecase.SignupInput
}

func (f *fakeService) Signup(_ context.Context, in usecase.SignupInput) (usecase.SignupOutcome, error) {
	f.gotInput = in
	return f.outcome, f.err
}

func (f *fakeService) Login(context.Context, string, string) (usersdomain.User, error) {
	return f.user, f.err
}

func call(t *testing.T, svc Service, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	New(svc, slog.New(slog.DiscardHandler)).Register(mux)
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
