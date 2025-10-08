package usecase

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode/utf8"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/validate"
	usersdomain "github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

// GoogleSignInInput is what the app sends after Google sign-in. The consent
// fields matter only when the sign-in creates an account.
type GoogleSignInInput struct {
	IDToken     string
	IsAdult     bool
	AcceptTerms bool
	Locale      string
}

// GoogleSignInResult is a LoginResult that also says whether the account is new.
type GoogleSignInResult struct {
	LoginResult
	Created bool
}

// SignInWithGoogle logs in with a Google ID token, linking or creating the
// account:
//
//  1. A Google account linked before logs in to its account.
//  2. Otherwise, an account with the same email gets the Google account
//     linked. If that email was never verified, its password is removed and
//     its sessions end: whoever set that password never proved they own the
//     email, and the Google holder just did.
//  3. Otherwise a new account is created, which needs the same consent as
//     sign-up; without it the answer is validate.Errors.
//
// Google must be the authority for the email (authdomain.GoogleIdentity.OwnsEmail),
// else authdomain.ErrGoogleEmailNotOwned. That rule depends only on the token,
// never on our data, so the answer reveals nothing about who has an account.
func (s *Service) SignInWithGoogle(ctx context.Context, in GoogleSignInInput) (GoogleSignInResult, error) {
	if s.Google == nil {
		return GoogleSignInResult{}, authdomain.ErrGoogleUnavailable
	}
	if strings.TrimSpace(in.IDToken) == "" {
		return GoogleSignInResult{}, authdomain.ErrInvalidGoogleToken
	}
	id, err := s.Google.Verify(ctx, strings.TrimSpace(in.IDToken))
	if err != nil {
		return GoogleSignInResult{}, err
	}
	if !id.OwnsEmail() {
		return GoogleSignInResult{}, authdomain.ErrGoogleEmailNotOwned
	}

	created := false
	userID, err := s.Identities.FindGoogleUser(ctx, id.Subject)
	if errors.Is(err, authdomain.ErrNoIdentity) {
		var u usersdomain.User
		u, created, err = s.findOrCreateGoogleUser(ctx, id, in)
		if err != nil {
			return GoogleSignInResult{}, err
		}
		userID = u.ID
		link := authdomain.GoogleLink{UserID: u.ID, Subject: id.Subject, Now: s.Now(),
			DropPassword: u.EmailVerifiedAt == nil && u.PasswordHash != nil}
		if err := s.Identities.LinkGoogle(ctx, link); err != nil {
			return GoogleSignInResult{}, err
		}
	} else if err != nil {
		return GoogleSignInResult{}, err
	}

	u, err := s.Users.GetByID(ctx, userID) // read again: linking may have changed it
	if err != nil {
		return GoogleSignInResult{}, err
	}
	tokens, err := s.startSession(ctx, u.ID)
	if err != nil {
		return GoogleSignInResult{}, err
	}
	return GoogleSignInResult{LoginResult: LoginResult{User: u, Tokens: tokens}, Created: created}, nil
}

// maxUsernameTries bounds the attempts to find a free generated username.
const maxUsernameTries = 5

// findOrCreateGoogleUser returns the account with the Google email, or creates
// one with a generated username (changeable later) and no password.
func (s *Service) findOrCreateGoogleUser(ctx context.Context, id authdomain.GoogleIdentity, in GoogleSignInInput) (usersdomain.User, bool, error) {
	u, err := s.Users.GetByEmail(ctx, id.Email)
	if err == nil || !errors.Is(err, usersdomain.ErrNotFound) {
		return u, false, err
	}

	if in.Locale == "" {
		in.Locale = "en"
	}
	var v validate.Validator
	v.Check(in.IsAdult, "is_adult", "must_be_adult", "Addis Hiwot is for people aged 18 or older")
	v.Check(in.AcceptTerms, "accept_terms", "must_accept_terms", "the Terms and Privacy Policy must be accepted")
	v.Check(validate.OneOf(in.Locale, "en", "am"), "locale", "unsupported_locale", "must be en or am")
	if err := v.Err(); err != nil {
		return usersdomain.User{}, false, err
	}

	base := usernameBase(id.Email)
	now := s.Now().UTC()
	terms := authdomain.TermsVersion
	for try := range maxUsernameTries {
		username := base
		if try > 0 || authdomain.IsReservedUsername(base) {
			username = withRandomDigits(base)
		}
		u, err := s.Users.Create(ctx, usersdomain.NewUser{
			Email:            id.Email,
			Username:         username,
			DisplayName:      displayName(id.Name, username),
			Locale:           in.Locale,
			Timezone:         DefaultTimezone,
			AdultConfirmedAt: &now,
			TermsAcceptedAt:  &now,
			TermsVersion:     &terms,
		})
		switch {
		case err == nil:
			return u, true, nil
		case errors.Is(err, usersdomain.ErrUsernameTaken):
			continue
		case errors.Is(err, usersdomain.ErrEmailTaken): // another request created it just now
			u, err := s.Users.GetByEmail(ctx, id.Email)
			return u, false, err
		default:
			return usersdomain.User{}, false, err
		}
	}
	return usersdomain.User{}, false, fmt.Errorf("auth: no free username after %d tries", maxUsernameTries)
}

// usernameBase turns the email's local part into a valid username of at most
// 15 characters, leaving room for 4 random digits.
func usernameBase(email string) string {
	local, _, _ := strings.Cut(strings.ToLower(email), "@")
	var b strings.Builder
	for _, r := range local {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	name := strings.TrimLeft(b.String(), "0123456789_.") // must start with a letter
	if len(name) > 15 {
		name = name[:15]
	}
	if len(name) < 3 {
		name = "friend"
	}
	return name
}

func withRandomDigits(base string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(10_000))
	if err != nil {
		panic("crypto/rand failed: " + err.Error()) // never happens since Go 1.24
	}
	return fmt.Sprintf("%s%04d", base, n.Int64())
}

// displayName is Google's name cut to 50 letters, or the username without one.
func displayName(name, username string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return username
	}
	if utf8.RuneCountInString(name) > 50 {
		name = strings.TrimSpace(string([]rune(name)[:50]))
	}
	return name
}
