package usershttp

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

type fakeService struct {
	user domain.User
	err  error
}

func (f fakeService) Me(context.Context, uuid.UUID) (domain.User, error) { return f.user, f.err }

// asUser is a stand-in for the auth middleware that logs in as id.
func asUser(id uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(authdomain.WithPrincipal(r.Context(), authdomain.Principal{UserID: id})))
		})
	}
}

func get(svc Service, requireAuth func(http.Handler) http.Handler) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	New(svc, slog.New(slog.DiscardHandler), requireAuth).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/me", nil))
	return rec
}

func TestMe(t *testing.T) {
	hash := "$argon2id$secret"
	u := domain.User{ID: uuid.New(), Username: "kidista", PasswordHash: &hash}
	rec := get(fakeService{user: u}, asUser(u.ID))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"username":"kidista"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "argon2id") {
		t.Error("the password hash leaked")
	}
}

func TestMeForADeletedAccount(t *testing.T) {
	rec := get(fakeService{err: domain.ErrNotFound}, asUser(uuid.New()))
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestMeWithoutTheMiddlewareIsAnInternalError(t *testing.T) {
	noAuth := func(next http.Handler) http.Handler { return next }
	if rec := get(fakeService{}, noAuth); rec.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500 for a wiring mistake", rec.Code)
	}
}
