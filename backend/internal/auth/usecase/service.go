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

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/validate"
	usersdomain "github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

// DefaultTimezone is set at sign-up; onboarding (Day 23) lets the user change it.
const DefaultTimezone = "Africa/Addis_Ababa"

// Same rule as the users_username_format check in the database.
var usernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]{2,19}$`)

// Service signs people up and logs them in.
type Service struct {
	users     usersdomain.Repository
	hasher    authdomain.PasswordHasher
	now       func() time.Time
	dummyHash string
}

// NewService prepares a dummy hash of a random password. Logins for accounts
// that do not exist are checked against it, so a failed login takes as long
// whether or not the account exists.
func NewService(users usersdomain.Repository, hasher authdomain.PasswordHasher, now func() time.Time) (*Service, error) {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	dummy, err := hasher.Hash(hex.EncodeToString(random))
	if err != nil {
		return nil, fmt.Errorf("auth: dummy hash: %w", err)
	}
	return &Service{users: users, hasher: hasher, now: now, dummyHash: dummy}, nil
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
	v.Check(validate.Email(in.Email), "email", "invalid_email", "must be an email address")
	authdomain.CheckPassword(&v, in.Password, in.Username, in.Email)
	v.Check(in.IsAdult, "is_adult", "must_be_adult", "Addis Hiwot is for people aged 18 or older")
	v.Check(in.AcceptTerms, "accept_terms", "must_accept_terms", "the Terms and Privacy Policy must be accepted")
	v.Check(validate.OneOf(in.Locale, "en", "am"), "locale", "unsupported_locale", "must be en or am")
	if err := v.Err(); err != nil {
		return 0, err
	}

	// Hash before touching the database, so a duplicate email costs the same time.
	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return 0, err
	}
	now := s.now().UTC()
	terms := authdomain.TermsVersion
	_, err = s.users.Create(ctx, usersdomain.NewUser{
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

// Login checks an email or username and a password. Every failure that depends
// on the account (no such account, Google-only account, wrong password) returns
// authdomain.ErrInvalidCredentials after the same amount of hashing work.
func (s *Service) Login(ctx context.Context, identifier, password string) (usersdomain.User, error) {
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
		user, err = s.users.GetByEmail(ctx, identifier)
	} else {
		user, err = s.users.GetByUsername(ctx, identifier)
	}
	if err != nil && !errors.Is(err, usersdomain.ErrNotFound) {
		return usersdomain.User{}, err
	}
	if errors.Is(err, usersdomain.ErrNotFound) || user.PasswordHash == nil {
		_, _ = s.hasher.Verify(password, s.dummyHash) // same work as a real check
		return usersdomain.User{}, authdomain.ErrInvalidCredentials
	}

	ok, err := s.hasher.Verify(password, *user.PasswordHash)
	if err != nil {
		return usersdomain.User{}, fmt.Errorf("auth: verify stored hash: %w", err)
	}
	if !ok {
		return usersdomain.User{}, authdomain.ErrInvalidCredentials
	}
	return user, nil
}
