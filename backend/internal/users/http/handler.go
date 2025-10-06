// Package usershttp serves the account endpoints (GET /v1/me) and owns the
// public JSON shape of an account.
package usershttp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/httpx"
	"github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

// User is the public shape of an account. It never includes the password hash.
type User struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Locale      string    `json:"locale"`
	Timezone    string    `json:"timezone"`
	// EmailVerified is false until the email code was entered; the app then
	// shows the code screen instead of Home.
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

// ToJSON converts an account to its public shape.
func ToJSON(u domain.User) User {
	return User{ID: u.ID, Username: u.Username, Email: u.Email, DisplayName: u.DisplayName,
		Locale: u.Locale, Timezone: u.Timezone, EmailVerified: u.EmailVerifiedAt != nil, CreatedAt: u.CreatedAt}
}

// Service is what the handlers need from the users use cases.
type Service interface {
	Me(ctx context.Context, userID uuid.UUID) (domain.User, error)
}

// Handler serves the account endpoints.
type Handler struct {
	svc         Service
	logger      *slog.Logger
	requireAuth func(http.Handler) http.Handler
}

// New returns the handlers; requireAuth is the auth middleware.
func New(svc Service, logger *slog.Logger, requireAuth func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireAuth: requireAuth}
}

// Register adds the routes to mux, all behind requireAuth.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /v1/me", h.requireAuth(http.HandlerFunc(h.me)))
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	p, ok := authdomain.PrincipalFrom(r.Context())
	if !ok { // requireAuth always sets it; this guards against a wiring mistake
		httpx.WriteError(w, r, h.logger, errors.New("usershttp: no principal: route registered without requireAuth"))
		return
	}
	u, err := h.svc.Me(r.Context(), p.UserID)
	if errors.Is(err, domain.ErrNotFound) {
		httpx.WriteError(w, r, h.logger, httpx.NewError(http.StatusNotFound, httpx.CodeNotFound, "This account no longer exists."))
		return
	}
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]User{"user": ToJSON(u)})
}
