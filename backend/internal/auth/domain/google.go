package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Google sign-in errors.
var (
	// ErrInvalidGoogleToken covers a Google ID token that is forged, expired,
	// issued to another app or not issued by Google.
	ErrInvalidGoogleToken = errors.New("auth: invalid Google ID token")
	// ErrGoogleEmailNotOwned means Google cannot vouch that the person owns the
	// email today (see GoogleIdentity.OwnsEmail).
	ErrGoogleEmailNotOwned = errors.New("auth: Google cannot vouch for this email")
	// ErrGoogleUnavailable means Google sign-in is not configured.
	ErrGoogleUnavailable = errors.New("auth: Google sign-in is not configured")
	// ErrIdentityConflict means the account already has another Google account
	// linked, or that Google account is linked to another account.
	ErrIdentityConflict = errors.New("auth: Google account linked elsewhere")
	// ErrNoIdentity means no account is linked to the Google account.
	ErrNoIdentity = errors.New("auth: no linked account")
)

// GoogleIdentity is what a verified Google ID token says about the person.
type GoogleIdentity struct {
	Subject       string // "sub": Google's stable ID of the account; never reused
	Email         string
	EmailVerified bool
	HostedDomain  string // "hd": set for Google Workspace accounts
	Name          string
}

// OwnsEmail reports whether Google is the authority for the email, so the
// person is known to own it now: a Gmail address, or a verified address of a
// Google Workspace domain. Any other address was verified once, when the Google
// account was created, and may have changed hands since (Google's guidance on
// verifying ID tokens, read 26 Oct 2026).
func (g GoogleIdentity) OwnsEmail() bool {
	if !g.EmailVerified {
		return false
	}
	return strings.HasSuffix(strings.ToLower(g.Email), "@gmail.com") || g.HostedDomain != ""
}

// GoogleVerifier checks a Google ID token's signature, issuer, audience and
// expiry. It returns ErrInvalidGoogleToken for any token that fails.
type GoogleVerifier interface {
	Verify(ctx context.Context, idToken string) (GoogleIdentity, error)
}

// GoogleLink links a Google account to an account.
type GoogleLink struct {
	UserID  uuid.UUID
	Subject string
	// DropPassword removes the password and ends every session, in the same
	// transaction: used when the account's email was never verified, so the
	// password may have been set by someone else.
	DropPassword bool
	Now          time.Time
}

// IdentityRepository stores which Google account belongs to which account.
type IdentityRepository interface {
	// FindGoogleUser returns the account linked to a Google subject, or ErrNoIdentity.
	FindGoogleUser(ctx context.Context, subject string) (uuid.UUID, error)
	// LinkGoogle links the Google account and marks the email verified, in one
	// transaction. Linking the same pair again is not an error; any other
	// existing link gives ErrIdentityConflict.
	LinkGoogle(ctx context.Context, l GoogleLink) error
}
