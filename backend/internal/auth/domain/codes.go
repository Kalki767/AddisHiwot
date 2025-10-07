package domain

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// CodePurpose says what an email code is for.
type CodePurpose string

// Code purposes; they match the database check constraint.
const (
	PurposeVerifyEmail   CodePurpose = "verify_email"
	PurposeResetPassword CodePurpose = "reset_password"
)

// Email code rules. Why guessing stays impractical:
//   - a code is one of 100,000 values and allows MaxCodeAttempts guesses: 5 in 100,000;
//   - at most MaxCodesPerHour codes per person per hour: 25 guesses an hour in all,
//     so finding one code takes about 2,000 hours (83 days) on average, during
//     which the person receives every one of those emails;
//   - each code also dies after CodeTTL, and IP and account rate limits slow
//     every request on top of this.
const (
	CodeDigits      = 5
	CodeTTL         = 10 * time.Minute
	MaxCodeAttempts = 5
	MaxCodesPerHour = 5
)

// Code errors.
var (
	// ErrInvalidCode is the one answer for a wrong, expired, used-up or unknown
	// code, and for an email without an account, so the endpoint cannot be used
	// to find out who has an account.
	ErrInvalidCode = errors.New("auth: invalid or expired code")
	// ErrNoLiveCode means there is no unexpired code with attempts left.
	ErrNoLiveCode = errors.New("auth: no live code")
	// ErrCodeAlreadyUsed means another request used the code first.
	ErrCodeAlreadyUsed = errors.New("auth: code already used")
	// ErrTooManyCodes means MaxCodesPerHour codes were already sent this hour.
	ErrTooManyCodes = errors.New("auth: too many codes this hour")
)

// CodeAttempt is a live code whose attempt was just counted, ready to compare.
type CodeAttempt struct {
	ID   uuid.UUID
	Hash []byte
}

// CodeRepository stores email codes as hashes.
type CodeRepository interface {
	// Issue retires the person's live code for purpose and stores a new one.
	Issue(ctx context.Context, userID uuid.UUID, purpose CodePurpose, hash []byte, now, expiresAt time.Time) error
	// CountSince counts codes issued for purpose since a time.
	CountSince(ctx context.Context, userID uuid.UUID, purpose CodePurpose, since time.Time) (int, error)
	// UseAttempt counts one attempt on the live code and returns it to compare;
	// ErrNoLiveCode if there is none, it expired, or its attempts are used up.
	UseAttempt(ctx context.Context, userID uuid.UUID, purpose CodePurpose, now time.Time, maxAttempts int) (CodeAttempt, error)
	// Consume marks a code used; ErrCodeAlreadyUsed if another request was first.
	Consume(ctx context.Context, id uuid.UUID, now time.Time) error
}

// Notifier sends the auth emails. It knows what to say, not how mail is sent.
type Notifier interface {
	VerificationCode(ctx context.Context, to, name, locale, code string) error
	// PasswordResetCode is sent when someone asks to reset their password.
	PasswordResetCode(ctx context.Context, to, name, locale, code string) error
	// SignupAttempt tells an account owner that someone tried to sign up with
	// their email: they get a notice instead of a code.
	SignupAttempt(ctx context.Context, to, name, locale string) error
}

// NewCode returns a random code of CodeDigits digits; every value, including
// ones with leading zeros, is equally likely.
func NewCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(100_000))
	if err != nil {
		panic("crypto/rand failed: " + err.Error()) // never happens since Go 1.24
	}
	return fmt.Sprintf("%05d", n.Int64())
}

// HashCode is the HMAC-SHA256 of a code, bound to its purpose and person, so a
// code for one account or purpose never matches another.
func HashCode(secret []byte, purpose CodePurpose, userID uuid.UUID, code string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(string(purpose) + "\x00" + userID.String() + "\x00" + code))
	return mac.Sum(nil)
}

// CodeMatches compares a typed code with a stored hash in constant time.
func CodeMatches(secret []byte, purpose CodePurpose, userID uuid.UUID, code string, hash []byte) bool {
	return hmac.Equal(HashCode(secret, purpose, userID, code), hash)
}
