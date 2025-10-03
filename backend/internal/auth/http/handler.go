// Package authhttp serves the sign-up and login endpoints: it decodes the
// request, calls the use case and encodes the answer. No rules live here.
package authhttp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/usecase"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/httpx"
	usersdomain "github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

// CodeLoginFailed is the error code for any failed login; its value is INVALID_CREDENTIALS.
const CodeLoginFailed = "INVALID_CREDENTIALS"

// Service is what the handlers need from the use case layer.
type Service interface {
	Signup(ctx context.Context, in usecase.SignupInput) (usecase.SignupOutcome, error)
	Login(ctx context.Context, identifier, password string) (usersdomain.User, error)
}

// Handler serves /v1/auth/*.
type Handler struct {
	svc    Service
	logger *slog.Logger
}

// New returns the auth handlers.
func New(svc Service, logger *slog.Logger) *Handler { return &Handler{svc: svc, logger: logger} }

// Register adds the routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/signup", h.signup)
	mux.HandleFunc("POST /v1/auth/login", h.login)
}

type signupRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	IsAdult     bool   `json:"is_adult"`
	AcceptTerms bool   `json:"accept_terms"`
	Locale      string `json:"locale"`
}

// signupAccepted is the one answer to a valid sign-up, whether the email was
// new or already registered, so the endpoint cannot be used to find out who
// uses Addis Hiwot.
var signupAccepted = map[string]string{
	"status":  "verification_pending",
	"message": "Check your email for a 5-digit code.",
}

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}
	// The outcome (created or already registered) only decides which email is
	// sent, from Day 10 on; the client gets the same answer either way.
	_, err := h.svc.Signup(r.Context(), usecase.SignupInput{
		Username: req.Username, Email: req.Email, Password: req.Password,
		IsAdult: req.IsAdult, AcceptTerms: req.AcceptTerms, Locale: req.Locale,
	})
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, signupAccepted)
}

type loginRequest struct {
	Identifier string `json:"identifier"` // email or username
	Password   string `json:"password"`
}

type userJSON struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Locale      string    `json:"locale"`
	Timezone    string    `json:"timezone"`
	CreatedAt   time.Time `json:"created_at"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}
	user, err := h.svc.Login(r.Context(), req.Identifier, req.Password)
	if errors.Is(err, authdomain.ErrInvalidCredentials) {
		httpx.WriteError(w, r, h.logger, httpx.NewError(http.StatusUnauthorized, CodeLoginFailed,
			"The email, username or password is not right."))
		return
	}
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]userJSON{"user": {
		ID: user.ID, Username: user.Username, Email: user.Email, DisplayName: user.DisplayName,
		Locale: user.Locale, Timezone: user.Timezone, CreatedAt: user.CreatedAt,
	}})
}
