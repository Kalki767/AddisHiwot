// Package domain holds the rules of signing up and logging in: the password
// policy, the password-hasher interface and the auth errors. It has no database,
// HTTP or crypto-library code; the usecase package builds on it.
package domain

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/validate"
)

// ErrInvalidCredentials is the one answer for a failed login, whether the
// account does not exist or the password is wrong, so nobody can learn which
// emails or usernames have an account.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// TermsVersion is the version of the terms and privacy policy shown at sign-up.
// Raising it later lets the app ask existing users to accept the new version.
const TermsVersion = "2026-10"

// PasswordHasher turns a password into a stored hash and checks a password
// against one. Implementations must use a slow, salted algorithm and compare
// in constant time.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (bool, error)
}

// Password length limits, in characters (an Amharic letter counts as one).
const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
)

// CheckPassword records password problems on v. Length is what makes a password
// strong; there are no "must contain a symbol" rules, which make passwords
// harder to remember without making them much harder to guess.
func CheckPassword(v *validate.Validator, password, username, email string) {
	n := utf8.RuneCountInString(password)
	v.Check(n >= MinPasswordLength, "password", "too_short", "must have at least 8 characters")
	v.Check(n <= MaxPasswordLength, "password", "too_long", "must have at most 128 characters")

	lower := strings.ToLower(password)
	v.Check(!commonPasswords[lower], "password", "too_common", "is one of the most common passwords")
	if u := strings.ToLower(strings.TrimSpace(username)); u != "" {
		v.Check(!strings.Contains(lower, u), "password", "contains_username", "must not contain the username")
	}
	if local, _, ok := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@"); ok && len(local) >= 3 {
		v.Check(!strings.Contains(lower, local), "password", "contains_email", "must not contain the email address")
	}
}

// IsReservedUsername reports whether a username belongs to the app or its team
// (in any letter case), so nobody can pose as staff or as AddisBot.
func IsReservedUsername(username string) bool {
	return reservedUsernames[strings.ToLower(strings.TrimSpace(username))]
}

// reservedUsernames are names of the app, its bot and its team, plus the
// obvious variants people would use to pose as them.
var reservedUsernames = map[string]bool{
	"admin": true, "administrator": true, "addisbot": true, "addis.bot": true, "addis_bot": true,
	"addishiwot": true, "addis.hiwot": true, "addis_hiwot": true, "support": true, "help": true,
	"moderator": true, "mod": true, "staff": true, "team": true, "official": true, "system": true,
	"root": true, "security": true, "privacy": true, "counselor": true, "counsellor": true,
}

// commonPasswords is a short starter list of very common passwords of 8 or more
// characters, plus the app's own name. A check against a full list of breached
// passwords is planned before the beta.
var commonPasswords = map[string]bool{
	"password": true, "password1": true, "password123": true, "passw0rd": true,
	"12345678": true, "123456789": true, "1234567890": true, "87654321": true,
	"11111111": true, "00000000": true, "12341234": true, "11223344": true,
	"qwertyui": true, "qwerty123": true, "qwertyuiop": true, "asdfghjk": true,
	"abcd1234": true, "1q2w3e4r": true, "iloveyou": true, "sunshine": true,
	"princess": true, "football": true, "baseball": true, "welcome1": true,
	"letmein1": true, "trustno1": true, "superman": true, "starwars": true,
	"addishiwot": true, "addis hiwot": true, "ethiopia": true, "ethiopia1": true,
}
