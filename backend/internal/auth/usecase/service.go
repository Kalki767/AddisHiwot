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
	"log/slog"
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

// Deps are what the Service needs; every field is required except Google.
type Deps struct {
	Users       usersdomain.Repository
	Sessions    authdomain.SessionRepository
	Codes       authdomain.CodeRepository
	ResetTokens authdomain.ResetTokenRepository
	Identities  authdomain.IdentityRepository
	// Google checks Google ID tokens; nil turns Google sign-in off.
	Google     authdomain.GoogleVerifier
	Hasher     authdomain.PasswordHasher
	Tokens     authdomain.AccessTokens
	Notifier   authdomain.Notifier
	CodeSecret []byte // keys the HMAC of email codes; at least 32 bytes
	Now        func() time.Time
	RefreshTTL time.Duration // how long a refresh token can be used
	Logger     *slog.Logger  // email failures are logged, never shown to the client
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
	if d.Users == nil || d.Sessions == nil || d.Codes == nil || d.ResetTokens == nil || d.Identities == nil || d.Hasher == nil || d.Tokens == nil ||
		d.Notifier == nil || d.Now == nil || d.Logger == nil || d.RefreshTTL <= 0 {
		return nil, errors.New("auth: every dependency is required")
	}
	if len(d.CodeSecret) < 32 {
		return nil, errors.New("auth: the code secret needs at least 32 bytes")
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
	user, err := s.Users.Create(ctx, usersdomain.NewUser{
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
		s.sendCode(ctx, user, authdomain.PurposeVerifyEmail)
		return Created, nil
	case errors.Is(err, usersdomain.ErrEmailTaken):
		s.notifyOwner(ctx, in.Email)
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

// sendCode issues a code and emails it, within MaxCodesPerHour. Failures are
// logged, not returned: the account exists either way, the person can ask for
// another code, and the answer to the client must not change.
func (s *Service) sendCode(ctx context.Context, u usersdomain.User, purpose authdomain.CodePurpose) {
	if err := s.issueAndEmail(ctx, u, purpose); err != nil {
		level := slog.LevelError
		if errors.Is(err, authdomain.ErrTooManyCodes) {
			level = slog.LevelInfo
		}
		s.Logger.Log(ctx, level, "email code not sent", slog.String("purpose", string(purpose)),
			slog.String("user_id", u.ID.String()), slog.Any("error", err))
	}
}

func (s *Service) issueAndEmail(ctx context.Context, u usersdomain.User, purpose authdomain.CodePurpose) error {
	now := s.Now()
	sent, err := s.Codes.CountSince(ctx, u.ID, purpose, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	if sent >= authdomain.MaxCodesPerHour {
		return authdomain.ErrTooManyCodes
	}
	code := authdomain.NewCode()
	hash := authdomain.HashCode(s.CodeSecret, purpose, u.ID, code)
	if err := s.Codes.Issue(ctx, u.ID, purpose, hash, now, now.Add(authdomain.CodeTTL)); err != nil {
		return err
	}
	if purpose == authdomain.PurposeResetPassword {
		return s.Notifier.PasswordResetCode(ctx, u.Email, u.DisplayName, u.Locale, code)
	}
	return s.Notifier.VerificationCode(ctx, u.Email, u.DisplayName, u.Locale, code)
}

// notifyOwner tells an account owner that someone tried to sign up with their
// email; failures are only logged.
func (s *Service) notifyOwner(ctx context.Context, email string) {
	owner, err := s.Users.GetByEmail(ctx, email)
	if err == nil {
		err = s.Notifier.SignupAttempt(ctx, owner.Email, owner.DisplayName, owner.Locale)
	}
	if err != nil {
		s.Logger.ErrorContext(ctx, "sign-up notice not sent", slog.Any("error", err))
	}
}

// ResendVerification emails a new code to an unverified account. It always
// returns nil for an unknown email, a verified account or the hourly limit, so
// the endpoint answers the same for everyone.
func (s *Service) ResendVerification(ctx context.Context, email string) error {
	u, err := s.Users.GetByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, usersdomain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if u.EmailVerifiedAt == nil {
		s.sendCode(ctx, u, authdomain.PurposeVerifyEmail)
	}
	return nil
}

var codePattern = regexp.MustCompile(`^[0-9]{5}$`)

// useCode finds the account, spends one attempt on its live code for purpose and
// compares it. Every failure that depends on the account (unknown email, no
// code, wrong, expired or used-up code) returns authdomain.ErrInvalidCode, so
// the caller cannot learn who has an account.
func (s *Service) useCode(ctx context.Context, email, code string, purpose authdomain.CodePurpose) (usersdomain.User, error) {
	u, err := s.Users.GetByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, usersdomain.ErrNotFound) {
		return usersdomain.User{}, authdomain.ErrInvalidCode
	}
	if err != nil {
		return usersdomain.User{}, err
	}
	now := s.Now()
	attempt, err := s.Codes.UseAttempt(ctx, u.ID, purpose, now, authdomain.MaxCodeAttempts)
	if errors.Is(err, authdomain.ErrNoLiveCode) {
		return usersdomain.User{}, authdomain.ErrInvalidCode
	}
	if err != nil {
		return usersdomain.User{}, err
	}
	if !authdomain.CodeMatches(s.CodeSecret, purpose, u.ID, code, attempt.Hash) {
		return usersdomain.User{}, authdomain.ErrInvalidCode
	}
	if err := s.Codes.Consume(ctx, attempt.ID, now); err != nil {
		if errors.Is(err, authdomain.ErrCodeAlreadyUsed) {
			return usersdomain.User{}, authdomain.ErrInvalidCode
		}
		return usersdomain.User{}, err
	}
	return u, nil
}

// VerifyEmail checks a code sent to email. On success the email is marked
// verified and a session starts, so the person is logged in. Every failure that
// depends on the account (unknown email, no code, wrong, expired or used-up
// code) returns authdomain.ErrInvalidCode.
func (s *Service) VerifyEmail(ctx context.Context, email, code string) (LoginResult, error) {
	code = strings.TrimSpace(code)
	var v validate.Validator
	v.Check(codePattern.MatchString(code), "code", "invalid_format", "must be 5 digits")
	if err := v.Err(); err != nil {
		return LoginResult{}, err
	}
	u, err := s.useCode(ctx, email, code, authdomain.PurposeVerifyEmail)
	if err != nil {
		return LoginResult{}, err
	}
	now := s.Now()
	if err := s.Users.MarkEmailVerified(ctx, u.ID, now); err != nil {
		return LoginResult{}, err
	}
	if u.EmailVerifiedAt == nil {
		u.EmailVerifiedAt = &now
	}
	tokens, err := s.startSession(ctx, u.ID)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: u, Tokens: tokens}, nil
}

// RequestPasswordReset emails a reset code. It returns nil for an unknown
// email, so the endpoint answers the same for everyone and cannot be used to
// find out who has an account.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	u, err := s.Users.GetByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, usersdomain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	s.sendCode(ctx, u, authdomain.PurposeResetPassword)
	return nil
}

// ResetGrant is the short-lived permission to set a new password.
type ResetGrant struct {
	Token     string
	ExpiresAt time.Time
}

// VerifyPasswordResetCode checks a reset code and returns a token that allows
// one password change. Every failure gives authdomain.ErrInvalidCode.
func (s *Service) VerifyPasswordResetCode(ctx context.Context, email, code string) (ResetGrant, error) {
	code = strings.TrimSpace(code)
	var v validate.Validator
	v.Check(codePattern.MatchString(code), "code", "invalid_format", "must be 5 digits")
	if err := v.Err(); err != nil {
		return ResetGrant{}, err
	}
	u, err := s.useCode(ctx, email, code, authdomain.PurposeResetPassword)
	if err != nil {
		return ResetGrant{}, err
	}
	now := s.Now()
	token, hash := authdomain.NewResetToken()
	expires := now.Add(authdomain.ResetTokenTTL)
	if err := s.ResetTokens.Create(ctx, u.ID, hash, now, expires); err != nil {
		return ResetGrant{}, err
	}
	return ResetGrant{Token: token, ExpiresAt: expires}, nil
}

// CompletePasswordReset sets a new password with the token from
// VerifyPasswordResetCode and ends every session of that account, so anyone who
// had stolen a token is logged out. The token works once.
//
// The new password must differ from the current one, as the design screen says.
// Reading the code proved the person can read the email, so the email counts as
// verified afterwards.
func (s *Service) CompletePasswordReset(ctx context.Context, token, newPassword string) error {
	if strings.TrimSpace(token) == "" {
		return authdomain.ErrInvalidResetToken
	}
	now := s.Now()
	grant, err := s.ResetTokens.Find(ctx, authdomain.HashResetToken(token), now)
	if err != nil {
		return err
	}
	u, err := s.Users.GetByID(ctx, grant.UserID)
	if err != nil {
		return err
	}

	var v validate.Validator
	authdomain.CheckPassword(&v, newPassword, u.Username, u.Email)
	if err := v.Err(); err != nil {
		return err
	}
	// Checked before the token is spent, so a rejected password can be retried.
	if u.PasswordHash != nil {
		same, err := s.Hasher.Verify(newPassword, *u.PasswordHash)
		if err != nil {
			return fmt.Errorf("auth: verify stored hash: %w", err)
		}
		if same {
			return validate.Errors{{Field: "password", Code: "same_as_current", Message: "must differ from your current password"}}
		}
	}
	hash, err := s.Hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	return s.ResetTokens.Complete(ctx, grant, hash, now)
}
