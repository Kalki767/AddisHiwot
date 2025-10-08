package usecase

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
)

var codeSecret = []byte("code-secret-that-is-32-bytes-long!")

// testDeps fills every dependency with an in-memory fake.
func testDeps(users *fakeUsers, sessions *fakeSessions, hasher authdomain.PasswordHasher, now func() time.Time) Deps {
	return Deps{Users: users, Sessions: sessions, Codes: newFakeCodes(), ResetTokens: newFakeResetTokens(users, sessions),
		Identities: newFakeIdentities(users, sessions), Google: fakeGoogle{}, Hasher: hasher, Tokens: fakeTokens{},
		Notifier: &fakeNotifier{}, CodeSecret: codeSecret, Now: now, RefreshTTL: 30 * 24 * time.Hour,
		Logger: slog.New(slog.DiscardHandler)}
}

// fakeCodes is an in-memory CodeRepository with the same rules as PostgreSQL's.
type fakeCodes struct {
	mu    sync.Mutex
	codes []*fakeCode
}

type fakeCode struct {
	id, user         uuid.UUID
	purpose          authdomain.CodePurpose
	hash             []byte
	created, expires time.Time
	attempts         int
	consumed         bool
}

func newFakeCodes() *fakeCodes { return &fakeCodes{} }

func (f *fakeCodes) Issue(_ context.Context, user uuid.UUID, p authdomain.CodePurpose, hash []byte, now, exp time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.codes {
		if c.user == user && c.purpose == p {
			c.consumed = true
		}
	}
	f.codes = append(f.codes, &fakeCode{id: uuid.New(), user: user, purpose: p, hash: hash, created: now, expires: exp})
	return nil
}

func (f *fakeCodes) CountSince(_ context.Context, user uuid.UUID, p authdomain.CodePurpose, since time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.codes {
		if c.user == user && c.purpose == p && !c.created.Before(since) {
			n++
		}
	}
	return n, nil
}

func (f *fakeCodes) UseAttempt(_ context.Context, user uuid.UUID, p authdomain.CodePurpose, now time.Time, maxAttempts int) (authdomain.CodeAttempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.codes {
		if c.user == user && c.purpose == p && !c.consumed && now.Before(c.expires) && c.attempts < maxAttempts {
			c.attempts++
			return authdomain.CodeAttempt{ID: c.id, Hash: c.hash}, nil
		}
	}
	return authdomain.CodeAttempt{}, authdomain.ErrNoLiveCode
}

func (f *fakeCodes) Consume(_ context.Context, id uuid.UUID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.codes {
		if c.id == id {
			if c.consumed {
				return authdomain.ErrCodeAlreadyUsed
			}
			c.consumed = true
			return nil
		}
	}
	return authdomain.ErrCodeAlreadyUsed
}

// fakeNotifier records what would have been emailed.
type fakeNotifier struct {
	codes      []string // verification codes sent, in order
	resetCodes []string // password-reset codes sent, in order
	notices    []string // emails that got a sign-up attempt notice
	locale     string
}

func (n *fakeNotifier) VerificationCode(_ context.Context, _, _, locale, code string) error {
	n.codes, n.locale = append(n.codes, code), locale
	return nil
}

func (n *fakeNotifier) PasswordResetCode(_ context.Context, _, _, locale, code string) error {
	n.resetCodes, n.locale = append(n.resetCodes, code), locale
	return nil
}

func (n *fakeNotifier) SignupAttempt(_ context.Context, to, _, _ string) error {
	n.notices = append(n.notices, to)
	return nil
}

func codeService(t *testing.T) (*Service, *fakeNotifier, *time.Time) {
	t.Helper()
	now := fixedNow
	svc, err := NewService(testDeps(newFakeUsers(), newFakeSessions(), &fakeHasher{}, func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	in := validInput()
	in.Locale = "am"
	if _, err := svc.Signup(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	return svc, svc.Notifier.(*fakeNotifier), &now
}

// Plan check: the verify flow.
func TestVerifyFlow(t *testing.T) {
	svc, mail, _ := codeService(t)
	if len(mail.codes) != 1 || mail.locale != "am" {
		t.Fatalf("sign-up must email one code in the account's language: %v %q", mail.codes, mail.locale)
	}
	res, err := svc.VerifyEmail(context.Background(), " KIDIST@example.com ", mail.codes[0])
	if err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if res.User.EmailVerifiedAt == nil || res.Tokens.AccessToken == "" {
		t.Errorf("want a verified user and a session: %+v", res)
	}
	if _, err := svc.VerifyEmail(context.Background(), "kidist@example.com", mail.codes[0]); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("reusing the code: err = %v, want ErrInvalidCode", err)
	}
}

func TestWrongCodesRunOut(t *testing.T) {
	svc, mail, _ := codeService(t)
	right := mail.codes[0]
	wrong := "00000"
	if right == wrong {
		wrong = "11111"
	}
	for i := range authdomain.MaxCodeAttempts {
		if _, err := svc.VerifyEmail(context.Background(), "kidist@example.com", wrong); !errors.Is(err, authdomain.ErrInvalidCode) {
			t.Fatalf("wrong attempt %d: err = %v", i+1, err)
		}
	}
	if _, err := svc.VerifyEmail(context.Background(), "kidist@example.com", right); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("right code after %d wrong ones: err = %v, want ErrInvalidCode", authdomain.MaxCodeAttempts, err)
	}
}

func TestCodesExpire(t *testing.T) {
	svc, mail, now := codeService(t)
	*now = now.Add(authdomain.CodeTTL)
	if _, err := svc.VerifyEmail(context.Background(), "kidist@example.com", mail.codes[0]); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("err = %v, want ErrInvalidCode", err)
	}
}

func TestUnknownEmailLooksLikeAWrongCode(t *testing.T) {
	svc, _, _ := codeService(t)
	if _, err := svc.VerifyEmail(context.Background(), "nobody@example.com", "12345"); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("err = %v, want ErrInvalidCode", err)
	}
}

func TestCodeMustBeFiveDigits(t *testing.T) {
	svc, _, _ := codeService(t)
	for _, c := range []string{"", "1234", "123456", "12a45"} {
		if _, err := svc.VerifyEmail(context.Background(), "kidist@example.com", c); fieldCodes(err)["code"] != "invalid_format" {
			t.Errorf("code %q: err = %v", c, err)
		}
	}
}

func TestResendSendsANewCodeAndOnlyTheNewestWorks(t *testing.T) {
	svc, mail, _ := codeService(t)
	if err := svc.ResendVerification(context.Background(), "kidist@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(mail.codes) != 2 {
		t.Fatalf("codes sent = %d, want 2", len(mail.codes))
	}
	if mail.codes[0] != mail.codes[1] {
		if _, err := svc.VerifyEmail(context.Background(), "kidist@example.com", mail.codes[0]); !errors.Is(err, authdomain.ErrInvalidCode) {
			t.Error("the older code must stop working")
		}
	}
	if _, err := svc.VerifyEmail(context.Background(), "kidist@example.com", mail.codes[1]); err != nil {
		t.Errorf("newest code: %v", err)
	}
}

func TestResendIsSilentWhenThereIsNothingToSend(t *testing.T) {
	svc, mail, _ := codeService(t)
	if err := svc.ResendVerification(context.Background(), "nobody@example.com"); err != nil {
		t.Errorf("unknown email: %v", err)
	}
	if _, err := svc.VerifyEmail(context.Background(), "kidist@example.com", mail.codes[0]); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResendVerification(context.Background(), "kidist@example.com"); err != nil {
		t.Errorf("verified account: %v", err)
	}
	if len(mail.codes) != 1 {
		t.Errorf("codes sent = %d, want only the sign-up code", len(mail.codes))
	}
}

func TestAtMostFiveCodesAnHour(t *testing.T) {
	svc, mail, now := codeService(t)
	for range 10 {
		_ = svc.ResendVerification(context.Background(), "kidist@example.com")
	}
	if len(mail.codes) != authdomain.MaxCodesPerHour {
		t.Errorf("codes sent = %d, want %d", len(mail.codes), authdomain.MaxCodesPerHour)
	}
	*now = now.Add(time.Hour + time.Minute)
	_ = svc.ResendVerification(context.Background(), "kidist@example.com")
	if len(mail.codes) != authdomain.MaxCodesPerHour+1 {
		t.Error("an hour later a new code must be allowed")
	}
}

func TestDuplicateSignupNotifiesTheOwnerInsteadOfSendingACode(t *testing.T) {
	svc, mail, _ := codeService(t)
	again := validInput()
	again.Username = "someoneelse"
	if _, err := svc.Signup(context.Background(), again); err != nil {
		t.Fatal(err)
	}
	if len(mail.codes) != 1 || len(mail.notices) != 1 || mail.notices[0] != "kidist@example.com" {
		t.Errorf("codes %v, notices %v; want no new code and one notice to the owner", mail.codes, mail.notices)
	}
}
