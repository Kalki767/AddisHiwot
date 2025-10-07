package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
)

// fakeResetTokens is an in-memory ResetTokenRepository.
type fakeResetTokens struct {
	mu     sync.Mutex
	tokens map[string]*fakeResetToken
}

type fakeResetToken struct {
	id, user uuid.UUID
	expires  time.Time
	used     bool
}

func newFakeResetTokens() *fakeResetTokens {
	return &fakeResetTokens{tokens: map[string]*fakeResetToken{}}
}

func (f *fakeResetTokens) Create(_ context.Context, user uuid.UUID, hash []byte, _, exp time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[string(hash)] = &fakeResetToken{id: uuid.New(), user: user, expires: exp}
	return nil
}

func (f *fakeResetTokens) Find(_ context.Context, hash []byte, now time.Time) (authdomain.ResetToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokens[string(hash)]
	if !ok || t.used || !now.Before(t.expires) {
		return authdomain.ResetToken{}, authdomain.ErrInvalidResetToken
	}
	return authdomain.ResetToken{ID: t.id, UserID: t.user}, nil
}

func (f *fakeResetTokens) Consume(_ context.Context, id uuid.UUID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tokens {
		if t.id == id {
			if t.used {
				return authdomain.ErrInvalidResetToken
			}
			t.used = true
			return nil
		}
	}
	return authdomain.ErrInvalidResetToken
}

// resetService has one signed-up, logged-in account and a movable clock.
func resetService(t *testing.T) (*Service, *fakeNotifier, *fakeSessions, *time.Time) {
	t.Helper()
	now := fixedNow
	sessions := newFakeSessions()
	svc, err := NewService(testDeps(newFakeUsers(), sessions, &fakeHasher{}, func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc.Signup(ctx, validInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "kidista", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	return svc, svc.Notifier.(*fakeNotifier), sessions, &now
}

// grant runs the request and verify steps and returns the reset token.
func grant(t *testing.T, svc *Service, mail *fakeNotifier) string {
	t.Helper()
	ctx := context.Background()
	if err := svc.RequestPasswordReset(ctx, "kidist@example.com"); err != nil {
		t.Fatal(err)
	}
	g, err := svc.VerifyPasswordResetCode(ctx, "kidist@example.com", mail.resetCodes[len(mail.resetCodes)-1])
	if err != nil {
		t.Fatal(err)
	}
	return g.Token
}

func TestPasswordResetFlow(t *testing.T) {
	svc, mail, sessions, _ := resetService(t)
	ctx := context.Background()
	token := grant(t, svc, mail)
	if len(mail.codes) != 1 || len(mail.resetCodes) != 1 {
		t.Fatalf("want one sign-up code and one reset code, got %v / %v", mail.codes, mail.resetCodes)
	}

	if err := svc.CompletePasswordReset(ctx, token, "a brand new passphrase"); err != nil {
		t.Fatalf("CompletePasswordReset: %v", err)
	}
	if _, err := svc.Login(ctx, "kidista", "a brand new passphrase"); err != nil {
		t.Errorf("login with the new password: %v", err)
	}
	if _, err := svc.Login(ctx, "kidista", "correct horse battery"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("the old password still works: %v", err)
	}
	// Every session from before the reset is gone.
	var found bool
	for _, reason := range sessions.revoked {
		found = true
		if reason != authdomain.RevokePasswordReset {
			t.Errorf("revoke reason = %q", reason)
		}
	}
	if !found {
		t.Error("the sessions open before the reset were not revoked")
	}
}

func TestResetTokenWorksOnce(t *testing.T) {
	svc, mail, _, _ := resetService(t)
	ctx := context.Background()
	token := grant(t, svc, mail)
	if err := svc.CompletePasswordReset(ctx, token, "a brand new passphrase"); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompletePasswordReset(ctx, token, "yet another passphrase"); !errors.Is(err, authdomain.ErrInvalidResetToken) {
		t.Errorf("err = %v, want ErrInvalidResetToken", err)
	}
}

func TestResetTokenExpires(t *testing.T) {
	svc, mail, _, now := resetService(t)
	token := grant(t, svc, mail)
	*now = now.Add(authdomain.ResetTokenTTL + time.Second)
	if err := svc.CompletePasswordReset(context.Background(), token, "a brand new passphrase"); !errors.Is(err, authdomain.ErrInvalidResetToken) {
		t.Errorf("err = %v, want ErrInvalidResetToken", err)
	}
}

func TestUnknownResetTokenIsRejected(t *testing.T) {
	svc, _, _, _ := resetService(t)
	for _, token := range []string{"", "   ", "made-up-token"} {
		if err := svc.CompletePasswordReset(context.Background(), token, "a brand new passphrase"); !errors.Is(err, authdomain.ErrInvalidResetToken) {
			t.Errorf("token %q: err = %v", token, err)
		}
	}
}

// The design says the new password must differ from the old one.
func TestNewPasswordMustDifferFromTheCurrentOne(t *testing.T) {
	svc, mail, _, _ := resetService(t)
	ctx := context.Background()
	token := grant(t, svc, mail)
	if got := fieldCodes(svc.CompletePasswordReset(ctx, token, "correct horse battery"))["password"]; got != "same_as_current" {
		t.Fatalf("password problem = %q, want same_as_current", got)
	}
	// The token was not spent, so the person can simply try another password.
	if err := svc.CompletePasswordReset(ctx, token, "a brand new passphrase"); err != nil {
		t.Errorf("retry with a different password: %v", err)
	}
}

func TestWeakNewPasswordIsRejectedWithoutSpendingTheToken(t *testing.T) {
	svc, mail, _, _ := resetService(t)
	ctx := context.Background()
	token := grant(t, svc, mail)
	if got := fieldCodes(svc.CompletePasswordReset(ctx, token, "short"))["password"]; got != "too_short" {
		t.Fatalf("password problem = %q, want too_short", got)
	}
	if err := svc.CompletePasswordReset(ctx, token, "a brand new passphrase"); err != nil {
		t.Errorf("retry with a good password: %v", err)
	}
}

// Five wrong codes use up the live code, as for email verification.
func TestWrongResetCodesRunOut(t *testing.T) {
	svc, mail, _, _ := resetService(t)
	ctx := context.Background()
	if err := svc.RequestPasswordReset(ctx, "kidist@example.com"); err != nil {
		t.Fatal(err)
	}
	right := mail.resetCodes[0]
	wrong := "00000"
	if right == wrong {
		wrong = "11111"
	}
	for i := range authdomain.MaxCodeAttempts {
		if _, err := svc.VerifyPasswordResetCode(ctx, "kidist@example.com", wrong); !errors.Is(err, authdomain.ErrInvalidCode) {
			t.Fatalf("wrong attempt %d: err = %v", i+1, err)
		}
	}
	if _, err := svc.VerifyPasswordResetCode(ctx, "kidist@example.com", right); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("right code after %d wrong ones: err = %v", authdomain.MaxCodeAttempts, err)
	}
}

func TestResetCodeExpires(t *testing.T) {
	svc, mail, _, now := resetService(t)
	ctx := context.Background()
	if err := svc.RequestPasswordReset(ctx, "kidist@example.com"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(authdomain.CodeTTL)
	if _, err := svc.VerifyPasswordResetCode(ctx, "kidist@example.com", mail.resetCodes[0]); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("err = %v, want ErrInvalidCode", err)
	}
}

// An unknown email must look exactly like a known one, at both steps.
func TestUnknownEmailLooksTheSame(t *testing.T) {
	svc, mail, _, _ := resetService(t)
	ctx := context.Background()
	if err := svc.RequestPasswordReset(ctx, "nobody@example.com"); err != nil {
		t.Errorf("request for an unknown email: %v, want nil", err)
	}
	if len(mail.resetCodes) != 0 {
		t.Error("a code was sent for an email without an account")
	}
	if _, err := svc.VerifyPasswordResetCode(ctx, "nobody@example.com", "12345"); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("verify for an unknown email: err = %v, want ErrInvalidCode", err)
	}
}

// A verification code must not work as a reset code, and the other way round.
func TestCodesDoNotCrossPurposes(t *testing.T) {
	svc, mail, _, _ := resetService(t)
	ctx := context.Background()
	if err := svc.RequestPasswordReset(ctx, "kidist@example.com"); err != nil {
		t.Fatal(err)
	}
	signupCode, resetCode := mail.codes[0], mail.resetCodes[0]
	if signupCode == resetCode {
		t.Skip("the two random codes happened to be equal")
	}
	if _, err := svc.VerifyPasswordResetCode(ctx, "kidist@example.com", signupCode); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("sign-up code accepted for a reset: %v", err)
	}
	if _, err := svc.VerifyEmail(ctx, "kidist@example.com", resetCode); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("reset code accepted for verification: %v", err)
	}
}

// Resetting also verifies the email: reading the code proved the person can
// read that inbox.
func TestResetVerifiesTheEmail(t *testing.T) {
	svc, mail, _, _ := resetService(t)
	ctx := context.Background()
	token := grant(t, svc, mail)
	if err := svc.CompletePasswordReset(ctx, token, "a brand new passphrase"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Login(ctx, "kidista", "a brand new passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if res.User.EmailVerifiedAt == nil {
		t.Error("the email should count as verified after a reset")
	}
}

func TestResetTokenIsOpaqueAndLong(t *testing.T) {
	svc, mail, _, _ := resetService(t)
	token := grant(t, svc, mail)
	if len(token) != 43 || strings.ContainsAny(token, "+/=") {
		t.Errorf("token %q is not 43 URL-safe characters", token)
	}
}
