// Package authhttp serves the sign-up and login endpoints: it decodes the
// request, calls the use case and encodes the answer. No rules live here.
package authhttp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/usecase"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/httpx"
	usershttp "github.com/Kalki767/AddisHiwot/backend/internal/users/http"
)

// Error codes of the auth endpoints and middleware.
const (
	CodeLoginFailed         = "INVALID_CREDENTIALS"   // any failed login
	CodeInvalidRefreshToken = "INVALID_REFRESH_TOKEN" // log in again
	CodeUnauthenticated     = "UNAUTHENTICATED"       // no access token sent
	CodeTokenExpired        = "TOKEN_EXPIRED"         // refresh, then retry
	CodeInvalidToken        = "INVALID_TOKEN"         // malformed, forged or not ours
)

// Service is what the handlers need from the use case layer.
type Service interface {
	Signup(ctx context.Context, in usecase.SignupInput) (usecase.SignupOutcome, error)
	Login(ctx context.Context, identifier, password string) (usecase.LoginResult, error)
	Refresh(ctx context.Context, refreshToken string) (usecase.Tokens, error)
	Logout(ctx context.Context, refreshToken string) error
	Authenticate(accessToken string) (authdomain.Principal, error)
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
	mux.HandleFunc("POST /v1/auth/refresh", h.refresh)
	mux.HandleFunc("POST /v1/auth/logout", h.logout)
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

type tokensJSON struct {
	TokenType             string    `json:"token_type"`
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}

func toTokensJSON(t usecase.Tokens) tokensJSON {
	return tokensJSON{TokenType: "Bearer", AccessToken: t.AccessToken, AccessTokenExpiresAt: t.AccessExpiresAt,
		RefreshToken: t.RefreshToken, RefreshTokenExpiresAt: t.RefreshExpiresAt}
}

// writeTokens sends a response holding tokens; no cache may keep a copy.
func writeTokens(w http.ResponseWriter, body any) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, body)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}
	res, err := h.svc.Login(r.Context(), req.Identifier, req.Password)
	if errors.Is(err, authdomain.ErrInvalidCredentials) {
		httpx.WriteError(w, r, h.logger, httpx.NewError(http.StatusUnauthorized, CodeLoginFailed,
			"The email, username or password is not right."))
		return
	}
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}
	writeTokens(w, struct {
		User   usershttp.User `json:"user"`
		Tokens tokensJSON     `json:"tokens"`
	}{User: usershttp.ToJSON(res.User), Tokens: toTokensJSON(res.Tokens)})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}
	tokens, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if errors.Is(err, authdomain.ErrRefreshTokenReused) {
		// The session is already revoked; the log records that a copied token was used.
		h.logger.WarnContext(r.Context(), "refresh token reuse detected: session revoked",
			slog.String("request_id", httpx.RequestIDFrom(r.Context())))
	}
	if errors.Is(err, authdomain.ErrInvalidRefreshToken) || errors.Is(err, authdomain.ErrRefreshTokenReused) {
		httpx.WriteError(w, r, h.logger, httpx.NewError(http.StatusUnauthorized, CodeInvalidRefreshToken,
			"The session has ended. Please log in again."))
		return
	}
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}
	writeTokens(w, map[string]tokensJSON{"tokens": toTokensJSON(tokens)})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}
	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RequireAuth lets a request through only with a valid access token in
// "Authorization: Bearer <token>", and stores the logged-in user in the
// request context (read it with authdomain.PrincipalFrom).
func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="addishiwot"`)
			httpx.WriteError(w, r, h.logger, httpx.NewError(http.StatusUnauthorized, CodeUnauthenticated, "Log in to continue."))
			return
		}
		p, err := h.svc.Authenticate(strings.TrimSpace(token))
		switch {
		case errors.Is(err, authdomain.ErrTokenExpired):
			w.Header().Set("WWW-Authenticate", `Bearer realm="addishiwot", error="invalid_token", error_description="expired"`)
			httpx.WriteError(w, r, h.logger, httpx.NewError(http.StatusUnauthorized, CodeTokenExpired, "The access token has expired. Refresh it and try again."))
		case errors.Is(err, authdomain.ErrInvalidToken):
			w.Header().Set("WWW-Authenticate", `Bearer realm="addishiwot", error="invalid_token"`)
			httpx.WriteError(w, r, h.logger, httpx.NewError(http.StatusUnauthorized, CodeInvalidToken, "The access token is not valid."))
		case err != nil:
			httpx.WriteError(w, r, h.logger, err)
		default:
			next.ServeHTTP(w, r.WithContext(authdomain.WithPrincipal(r.Context(), p)))
		}
	})
}
