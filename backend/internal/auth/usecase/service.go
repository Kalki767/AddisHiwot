// Package usecase holds the sign-up and login rules. It depends only on the
// domain interfaces (users.Repository, auth.PasswordHasher), so its tests use
// in-memory fakes and it never sees PostgreSQL, Argon2 or HTTP.
package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/validate"
	usersdomain "github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

// DefaultTimezone is set at sign-up; onboarding (Day 23) lets the user change it.
const DefaultTimezone = "Africa/Addis_Ababa"

// Same rule as the users_username_format check in the database.
var usernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]{2,19}$`)

// Deps are what the Service needs; every field is required.
type Deps struct {
	Users      usersdomain.Repository
	Sessions   authdomain.SessionRepository
	Hasher     authdomain.PasswordHasher
	Tokens     authdomain.AccessTokens
	Now        func() time.Time
	RefreshTTL time.Duration // how long a refresh token can be used
}

// Service signs people up, logs them in and manages their sessions.
type Service struct {
	Deps
	dummyHash string
}

// NewService prepares a dummy hash of a random password. Logins for accounts
// that do not exist are checked against it, so a failed login takes as long
// whether or not the account exists.
func NewService(d Deps) (*Service, error) {
	if d.Users == nil || d.Sessions == nil || d.Hasher == nil || d.Tokens == nil || d.Now == nil || d.RefreshTTL <= 0 {
		return nil, errors.New("auth: every dependency is required")
	}
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	dummy, err := d.Hasher.Hash(hex.EncodeToString(random))
	if err != nil {
		return nil, fmt.Errorf("auth: dummy hash: %w", err)
	}
	return &Service{Deps: d, dummyHash: dummy}, nil
}

// Tokens is what a client keeps after logging in or refreshing.
type Tokens struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// LoginResult is the logged-in user and the tokens of the new session.
type LoginResult struct {
	User   usersdomain.User
	Tokens Tokens
}

// SignupInput is what the sign-up screen sends.
type SignupInput struct {
	Username    string
	Email       string
	Password    string
	IsAdult     bool // "I am 18 or older"
	AcceptTerms bool // "I agree to the Terms and Privacy Policy"
	Locale      string
}

// SignupOutcome tells the caller what happened. Callers must answer the client
// the same way for both outcomes, so nobody can learn which emails have an
// account; the outcome only decides which email is sent (Day 10).
type SignupOutcome int

// Sign-up outcomes.
const (
	Created SignupOutcome = iota + 1
	EmailAlreadyRegistered
)

// Signup validates the input, hashes the password and creates the account.
// It returns validate.Errors for invalid input (including a taken username).
func (s *Service) Signup(ctx context.Context, in SignupInput) (SignupOutcome, error) {
	in.Username = strings.TrimSpace(in.Username)
	in.Email = strings.TrimSpace(in.Email)
	if in.Locale == "" {
		in.Locale = "en"
	}

	var v validate.Validator
	v.Check(usernamePattern.MatchString(in.Username), "username", "invalid_username",
		"must be 3 to 20 letters, digits, '_' or '.', starting with a letter")
	v.Check(!authdomain.IsReservedUsername(in.Username), "username", "username_reserved", "is reserved for the Addis Hiwot team")
	v.Check(validate.Email(in.Email), "email", "invalid_email", "must be an email address")
	authdomain.CheckPassword(&v, in.Password, in.Username, in.Email)
	v.Check(in.IsAdult, "is_adult", "must_be_adult", "Addis Hiwot is for people aged 18 or older")
	v.Check(in.AcceptTerms, "accept_terms", "must_accept_terms", "the Terms and Privacy Policy must be accepted")
	v.Check(validate.OneOf(in.Locale, "en", "am"), "locale", "unsupported_locale", "must be en or am")
	if err := v.Err(); err != nil {
		return 0, err
	}

	// Hash before touching the database, so a duplicate email costs the same time.
	hash, err := s.Hasher.Hash(in.Password)
	if err != nil {
		return 0, err
	}
	now := s.Now().UTC()
	terms := authdomain.TermsVersion
	_, err = s.Users.Create(ctx, usersdomain.NewUser{
		Email:            in.Email,
		Username:         in.Username,
		PasswordHash:     &hash,
		DisplayName:      in.Username,
		Locale:           in.Locale,
		Timezone:         DefaultTimezone,
		AdultConfirmedAt: &now,
		TermsAcceptedAt:  &now,
		TermsVersion:     &terms,
	})
	switch {
	case err == nil:
		return Created, nil
	case errors.Is(err, usersdomain.ErrEmailTaken):
		return EmailAlreadyRegistered, nil
	case errors.Is(err, usersdomain.ErrUsernameTaken):
		return 0, validate.Errors{{Field: "username", Code: "username_taken", Message: "is already taken"}}
	default:
		return 0, err
	}
}

// Login checks an email or username and a password and starts a session.
// Every failure that depends on the account (no such account, Google-only
// account, wrong password) returns authdomain.ErrInvalidCredentials after the
// same amount of hashing work.
func (s *Service) Login(ctx context.Context, identifier, password string) (LoginResult, error) {
	user, err := s.checkCredentials(ctx, identifier, password)
	if err != nil {
		return LoginResult{}, err
	}
	tokens, err := s.startSession(ctx, user.ID)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: user, Tokens: tokens}, nil
}

func (s *Service) checkCredentials(ctx context.Context, identifier, password string) (usersdomain.User, error) {
	identifier = strings.TrimSpace(identifier)
	var v validate.Validator
	v.Check(identifier != "", "identifier", "required", "enter your email or username")
	v.Check(password != "", "password", "required", "enter your password")
	if err := v.Err(); err != nil {
		return usersdomain.User{}, err
	}

	// Usernames can never contain '@', so '@' means an email.
	var user usersdomain.User
	var err error
	if strings.Contains(identifier, "@") {
		user, err = s.Users.GetByEmail(ctx, identifier)
	} else {
		user, err = s.Users.GetByUsername(ctx, identifier)
	}
	if err != nil && !errors.Is(err, usersdomain.ErrNotFound) {
		return usersdomain.User{}, err
	}
	if errors.Is(err, usersdomain.ErrNotFound) || user.PasswordHash == nil {
		_, _ = s.Hasher.Verify(password, s.dummyHash) // same work as a real check
		return usersdomain.User{}, authdomain.ErrInvalidCredentials
	}

	ok, err := s.Hasher.Verify(password, *user.PasswordHash)
	if err != nil {
		return usersdomain.User{}, fmt.Errorf("auth: verify stored hash: %w", err)
	}
	if !ok {
		return usersdomain.User{}, authdomain.ErrInvalidCredentials
	}
	return user, nil
}

// startSession creates a session with its first refresh token and issues an access token.
func (s *Service) startSession(ctx context.Context, userID uuid.UUID) (Tokens, error) {
	now := s.Now()
	refresh, hash := authdomain.NewRefreshToken()
	refreshExp := now.Add(s.RefreshTTL)
	sessionID, err := s.Sessions.CreateSession(ctx, userID, hash, refreshExp)
	if err != nil {
		return Tokens{}, err
	}
	access, accessExp, err := s.Tokens.Issue(userID, sessionID, now)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: refresh, RefreshExpiresAt: refreshExp}, nil
}

// Refresh exchanges a refresh token for a new access token and a new refresh
// token (rotation). A used refresh token coming back means it was copied, so the
// whole session is revoked and authdomain.ErrRefreshTokenReused is returned —
// except a retry within authdomain.RefreshReuseGrace whose replacement was never
// used (a lost response), which gets a fresh token. An unknown, expired or
// revoked token gives authdomain.ErrInvalidRefreshToken.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	if refreshToken == "" {
		return Tokens{}, authdomain.ErrInvalidRefreshToken
	}
	rec, err := s.Sessions.GetRefreshToken(ctx, authdomain.HashRefreshToken(refreshToken))
	if err != nil {
		return Tokens{}, err
	}
	if rec.SessionRevoked {
		return Tokens{}, authdomain.ErrInvalidRefreshToken
	}
	now := s.Now()
	next, nextHash := authdomain.NewRefreshToken()
	nextExp := now.Add(s.RefreshTTL)
	if rec.UsedAt != nil {
		if !isLostResponseRetry(rec, now) {
			return Tokens{}, s.revokeForReuse(ctx, rec.SessionID)
		}
		// The client never received the token that replaced this one: replace it.
		if err := s.Sessions.Reissue(ctx, rec.ID, *rec.SuccessorID, rec.SessionID, nextHash, nextExp, now); err != nil {
			if errors.Is(err, authdomain.ErrRefreshTokenReused) {
				return Tokens{}, s.revokeForReuse(ctx, rec.SessionID)
			}
			return Tokens{}, err
		}
		return s.issueAccess(rec, next, nextExp, now)
	}
	if !now.Before(rec.ExpiresAt) {
		return Tokens{}, authdomain.ErrInvalidRefreshToken
	}

	if err := s.Sessions.Rotate(ctx, rec.ID, rec.SessionID, nextHash, nextExp, now); err != nil {
		if errors.Is(err, authdomain.ErrRefreshTokenReused) { // another request used it first
			return Tokens{}, s.revokeForReuse(ctx, rec.SessionID)
		}
		return Tokens{}, err
	}
	return s.issueAccess(rec, next, nextExp, now)
}

// isLostResponseRetry reports whether a used refresh token coming back looks
// like a client retrying after a lost response, not a copied token: it was used
// moments ago and the token that replaced it was never used.
func isLostResponseRetry(rec authdomain.RefreshTokenRecord, now time.Time) bool {
	return rec.UsedAt != nil && now.Sub(*rec.UsedAt) <= authdomain.RefreshReuseGrace &&
		rec.SuccessorID != nil && !rec.SuccessorUsed
}

func (s *Service) issueAccess(rec authdomain.RefreshTokenRecord, refresh string, refreshExp, now time.Time) (Tokens, error) {
	access, accessExp, err := s.Tokens.Issue(rec.UserID, rec.SessionID, now)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: refresh, RefreshExpiresAt: refreshExp}, nil
}

func (s *Service) revokeForReuse(ctx context.Context, sessionID uuid.UUID) error {
	if err := s.Sessions.RevokeSession(ctx, sessionID, authdomain.RevokeReuseDetected); err != nil {
		return err
	}
	return authdomain.ErrRefreshTokenReused
}

// Logout ends the session of a refresh token. An unknown or already ended
// session is not an error, so logout never reveals anything and can be retried.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	rec, err := s.Sessions.GetRefreshToken(ctx, authdomain.HashRefreshToken(refreshToken))
	if errors.Is(err, authdomain.ErrInvalidRefreshToken) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.Sessions.RevokeSession(ctx, rec.SessionID, authdomain.RevokeLogout)
}

// Authenticate checks an access token and returns who it belongs to. It returns
// authdomain.ErrTokenExpired (the client should refresh) or authdomain.ErrInvalidToken.
func (s *Service) Authenticate(accessToken string) (authdomain.Principal, error) {
	c, err := s.Tokens.Verify(accessToken, s.Now())
	if err != nil {
		return authdomain.Principal{}, err
	}
	return authdomain.Principal{UserID: c.UserID, SessionID: c.SessionID}, nil
}
