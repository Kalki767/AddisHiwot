package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/validate"
	usersdomain "github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

// fakeUsers is an in-memory users.Repository that, like PostgreSQL's citext,
// ignores letter case for email and username.
type fakeUsers struct {
	byID map[uuid.UUID]usersdomain.User
}

func newFakeUsers() *fakeUsers { return &fakeUsers{byID: map[uuid.UUID]usersdomain.User{}} }

func (f *fakeUsers) Create(_ context.Context, n usersdomain.NewUser) (usersdomain.User, error) {
	for _, u := range f.byID {
		if strings.EqualFold(u.Email, n.Email) {
			return usersdomain.User{}, usersdomain.ErrEmailTaken
		}
		if strings.EqualFold(u.Username, n.Username) {
			return usersdomain.User{}, usersdomain.ErrUsernameTaken
		}
	}
	u := usersdomain.User{ID: uuid.New(), Email: n.Email, Username: n.Username, PasswordHash: n.PasswordHash,
		DisplayName: n.DisplayName, Locale: n.Locale, Timezone: n.Timezone, AdultConfirmedAt: n.AdultConfirmedAt,
		TermsAcceptedAt: n.TermsAcceptedAt, TermsVersion: n.TermsVersion}
	f.byID[u.ID] = u
	return u, nil
}

func (f *fakeUsers) find(match func(usersdomain.User) bool) (usersdomain.User, error) {
	for _, u := range f.byID {
		if match(u) {
			return u, nil
		}
	}
	return usersdomain.User{}, usersdomain.ErrNotFound
}

func (f *fakeUsers) GetByID(_ context.Context, id uuid.UUID) (usersdomain.User, error) {
	return f.find(func(u usersdomain.User) bool { return u.ID == id })
}

func (f *fakeUsers) GetByEmail(_ context.Context, e string) (usersdomain.User, error) {
	return f.find(func(u usersdomain.User) bool { return strings.EqualFold(u.Email, e) })
}

func (f *fakeUsers) GetByUsername(_ context.Context, n string) (usersdomain.User, error) {
	return f.find(func(u usersdomain.User) bool { return strings.EqualFold(u.Username, n) })
}

// fakeHasher stores "hash:<password>" and counts the work done.
type fakeHasher struct{ hashes, verifies int }

func (h *fakeHasher) Hash(pw string) (string, error) { h.hashes++; return "hash:" + pw, nil }
func (h *fakeHasher) Verify(pw, encoded string) (bool, error) {
	h.verifies++
	return encoded == "hash:"+pw, nil
}

var fixedNow = time.Date(2026, 10, 26, 9, 0, 0, 0, time.FixedZone("EAT", 3*3600))

func newService(t *testing.T) (*Service, *fakeUsers, *fakeHasher) {
	t.Helper()
	users, hasher := newFakeUsers(), &fakeHasher{}
	svc, err := NewService(users, hasher, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	*hasher = fakeHasher{} // do not count the dummy hash made at start-up
	return svc, users, hasher
}

func validInput() SignupInput {
	return SignupInput{Username: "KidistA", Email: "kidist@example.com", Password: "correct horse battery",
		IsAdult: true, AcceptTerms: true}
}

func fieldCodes(err error) map[string]string {
	var errs validate.Errors
	if !errors.As(err, &errs) {
		return nil
	}
	out := map[string]string{}
	for _, e := range errs {
		out[e.Field] = e.Code
	}
	return out
}

func TestSignupCreatesTheAccount(t *testing.T) {
	svc, users, _ := newService(t)
	in := validInput()
	in.Username, in.Email = "  KidistA ", " kidist@example.com "
	outcome, err := svc.Signup(context.Background(), in)
	if err != nil || outcome != Created {
		t.Fatalf("Signup = %v, %v", outcome, err)
	}
	u, err := users.GetByEmail(context.Background(), "kidist@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "KidistA" || u.DisplayName != "KidistA" || u.Locale != "en" || u.Timezone != DefaultTimezone {
		t.Errorf("stored user %+v", u)
	}
	if *u.PasswordHash != "hash:correct horse battery" {
		t.Errorf("password not hashed: %q", *u.PasswordHash)
	}
	if !u.AdultConfirmedAt.Equal(fixedNow) || !u.TermsAcceptedAt.Equal(fixedNow) || *u.TermsVersion != authdomain.TermsVersion {
		t.Errorf("consent not stored with time and version: %v %v %v", u.AdultConfirmedAt, u.TermsAcceptedAt, *u.TermsVersion)
	}
	if u.AdultConfirmedAt.Location() != time.UTC {
		t.Errorf("consent time zone = %v, want UTC", u.AdultConfirmedAt.Location())
	}
}

func TestSignupWithARegisteredEmailLooksTheSameButCreatesNothing(t *testing.T) {
	svc, users, hasher := newService(t)
	if _, err := svc.Signup(context.Background(), validInput()); err != nil {
		t.Fatal(err)
	}
	again := validInput()
	again.Username, again.Email = "someoneelse", "KIDIST@example.com"
	outcome, err := svc.Signup(context.Background(), again)
	if err != nil || outcome != EmailAlreadyRegistered {
		t.Fatalf("Signup = %v, %v; want EmailAlreadyRegistered and no error", outcome, err)
	}
	if len(users.byID) != 1 {
		t.Errorf("accounts = %d, want 1", len(users.byID))
	}
	if hasher.hashes != 2 {
		t.Errorf("hashes = %d, want 2: the duplicate must cost the same hashing work", hasher.hashes)
	}
}

func TestSignupWithATakenUsername(t *testing.T) {
	svc, _, _ := newService(t)
	if _, err := svc.Signup(context.Background(), validInput()); err != nil {
		t.Fatal(err)
	}
	again := validInput()
	again.Email, again.Username = "other@example.com", "kidista"
	_, err := svc.Signup(context.Background(), again)
	if got := fieldCodes(err)["username"]; got != "username_taken" {
		t.Errorf("username problem = %q, want username_taken (err %v)", got, err)
	}
}

func TestSignupValidation(t *testing.T) {
	tests := map[string]struct {
		change      func(*SignupInput)
		field, code string
	}{
		"weak password":           {func(in *SignupInput) { in.Password = "short" }, "password", "too_short"},
		"common password":         {func(in *SignupInput) { in.Password = "password123" }, "password", "too_common"},
		"password with username":  {func(in *SignupInput) { in.Password = "kidista-rocks-2026" }, "password", "contains_username"},
		"missing adult confirm":   {func(in *SignupInput) { in.IsAdult = false }, "is_adult", "must_be_adult"},
		"terms not accepted":      {func(in *SignupInput) { in.AcceptTerms = false }, "accept_terms", "must_accept_terms"},
		"invalid email":           {func(in *SignupInput) { in.Email = "kidist@localhost" }, "email", "invalid_email"},
		"username with @":         {func(in *SignupInput) { in.Username = "kid@ist" }, "username", "invalid_username"},
		"username starting digit": {func(in *SignupInput) { in.Username = "2kidist" }, "username", "invalid_username"},
		"reserved username":       {func(in *SignupInput) { in.Username = "AddisBot" }, "username", "username_reserved"},
		"unsupported locale":      {func(in *SignupInput) { in.Locale = "fr" }, "locale", "unsupported_locale"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			svc, users, hasher := newService(t)
			in := validInput()
			tt.change(&in)
			_, err := svc.Signup(context.Background(), in)
			if got := fieldCodes(err)[tt.field]; got != tt.code {
				t.Errorf("%s problem = %q, want %q (err %v)", tt.field, got, tt.code, err)
			}
			if len(users.byID) != 0 || hasher.hashes != 0 {
				t.Error("invalid input must be rejected before hashing or storing")
			}
		})
	}
}

func TestSignupReportsAllProblemsAtOnce(t *testing.T) {
	svc, _, _ := newService(t)
	_, err := svc.Signup(context.Background(), SignupInput{})
	codes := fieldCodes(err)
	for _, f := range []string{"username", "email", "password", "is_adult", "accept_terms"} {
		if codes[f] == "" {
			t.Errorf("no problem reported for %s: %v", f, codes)
		}
	}
}

func signedUp(t *testing.T) (*Service, *fakeHasher) {
	t.Helper()
	svc, _, hasher := newService(t)
	if _, err := svc.Signup(context.Background(), validInput()); err != nil {
		t.Fatal(err)
	}
	*hasher = fakeHasher{}
	return svc, hasher
}

func TestLoginWithEmailOrUsername(t *testing.T) {
	for _, id := range []string{"kidist@example.com", "KIDIST@Example.com", "KidistA", "kidista", "  kidista  "} {
		svc, _ := signedUp(t)
		u, err := svc.Login(context.Background(), id, "correct horse battery")
		if err != nil || u.Username != "KidistA" {
			t.Errorf("Login(%q) = %+v, %v", id, u.Username, err)
		}
	}
}

func TestLoginWithAWrongPassword(t *testing.T) {
	svc, hasher := signedUp(t)
	_, err := svc.Login(context.Background(), "kidista", "wrong horse battery")
	if !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("err = %v, want ErrInvalidCredentials", err)
	}
	if hasher.verifies != 1 {
		t.Errorf("verifies = %d, want 1", hasher.verifies)
	}
}

// The plan's check: an unknown email gives the same error and still runs
// exactly one (dummy) hash comparison, like a wrong password does.
func TestLoginWithAnUnknownAccountRunsADummyCompare(t *testing.T) {
	for _, id := range []string{"nobody@example.com", "nobody"} {
		svc, hasher := signedUp(t)
		_, err := svc.Login(context.Background(), id, "correct horse battery")
		if !errors.Is(err, authdomain.ErrInvalidCredentials) {
			t.Errorf("Login(%q) err = %v, want ErrInvalidCredentials", id, err)
		}
		if hasher.verifies != 1 {
			t.Errorf("Login(%q) verifies = %d, want 1 dummy comparison", id, hasher.verifies)
		}
	}
}

func TestLoginToAGoogleOnlyAccountRunsADummyCompare(t *testing.T) {
	users, hasher := newFakeUsers(), &fakeHasher{}
	svc, err := NewService(users, hasher, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.Create(context.Background(), usersdomain.NewUser{Email: "g@example.com", Username: "googler"}); err != nil {
		t.Fatal(err)
	}
	*hasher = fakeHasher{}
	if _, err := svc.Login(context.Background(), "g@example.com", "anything-at-all"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("err = %v, want ErrInvalidCredentials", err)
	}
	if hasher.verifies != 1 {
		t.Errorf("verifies = %d, want 1", hasher.verifies)
	}
}

func TestLoginRequiresBothFields(t *testing.T) {
	svc, _ := signedUp(t)
	_, err := svc.Login(context.Background(), "  ", "")
	codes := fieldCodes(err)
	if codes["identifier"] != "required" || codes["password"] != "required" {
		t.Errorf("codes = %v", codes)
	}
}
