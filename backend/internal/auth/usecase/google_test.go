package usecase

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	usersdomain "github.com/Kalki767/AddisHiwot/backend/internal/users/domain"
)

// fakeGoogle accepts the tokens in googleTokens; any other token fails the
// way a forged or wrong-audience token fails the real verifier.
type fakeGoogle struct{}

var googleTokens = map[string]authdomain.GoogleIdentity{
	"new-gmail":       {Subject: "sub-new", Email: "Abebe.Kebede@gmail.com", EmailVerified: true, Name: "Abebe Kebede"},
	"kidist-gmail":    {Subject: "sub-kidist", Email: "kidist@gmail.com", EmailVerified: true, Name: "Kidist"},
	"kidist-other":    {Subject: "sub-kidist-2", Email: "kidist@gmail.com", EmailVerified: true},
	"workspace":       {Subject: "sub-ws", Email: "hana@school.edu.et", EmailVerified: true, HostedDomain: "school.edu.et"},
	"other-domain":    {Subject: "sub-x", Email: "someone@yahoo.com", EmailVerified: true},
	"unverified":      {Subject: "sub-u", Email: "u@gmail.com", EmailVerified: false},
	"reserved-name":   {Subject: "sub-admin", Email: "admin@gmail.com", EmailVerified: true},
	"digits-only":     {Subject: "sub-d", Email: "0911@gmail.com", EmailVerified: true},
	"kidist-new-mail": {Subject: "sub-kidist", Email: "kidist.new@gmail.com", EmailVerified: true},
}

func (fakeGoogle) Verify(_ context.Context, token string) (authdomain.GoogleIdentity, error) {
	if id, ok := googleTokens[token]; ok {
		return id, nil
	}
	return authdomain.GoogleIdentity{}, authdomain.ErrInvalidGoogleToken
}

// fakeIdentities is an in-memory IdentityRepository; LinkGoogle changes the
// fake users and sessions as the PostgreSQL transaction does.
type fakeIdentities struct {
	mu       sync.Mutex
	bySub    map[string]uuid.UUID
	users    *fakeUsers
	sessions *fakeSessions
}

func newFakeIdentities(users *fakeUsers, sessions *fakeSessions) *fakeIdentities {
	return &fakeIdentities{bySub: map[string]uuid.UUID{}, users: users, sessions: sessions}
}

func (f *fakeIdentities) FindGoogleUser(_ context.Context, sub string) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.bySub[sub]; ok {
		return id, nil
	}
	return uuid.Nil, authdomain.ErrNoIdentity
}

func (f *fakeIdentities) LinkGoogle(ctx context.Context, l authdomain.GoogleLink) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sub, user := range f.bySub {
		if (sub == l.Subject) != (user == l.UserID) {
			return authdomain.ErrIdentityConflict
		}
	}
	f.bySub[l.Subject] = l.UserID
	if l.DropPassword {
		_ = f.users.setPasswordHash(l.UserID, nil)
		f.sessions.revokeUser(l.UserID, authdomain.RevokeGoogleLinked)
	}
	return f.users.MarkEmailVerified(ctx, l.UserID, l.Now)
}

func googleService(t *testing.T) (*Service, *fakeUsers, *fakeSessions) {
	t.Helper()
	users, sessions := newFakeUsers(), newFakeSessions()
	svc, err := NewService(testDeps(users, sessions, &fakeHasher{}, func() time.Time { return fixedNow }))
	if err != nil {
		t.Fatal(err)
	}
	return svc, users, sessions
}

func consenting(token string) GoogleSignInInput {
	return GoogleSignInInput{IDToken: token, IsAdult: true, AcceptTerms: true, Locale: "am"}
}

func TestGoogleSignInCreatesANewAccount(t *testing.T) {
	svc, _, _ := googleService(t)
	res, err := svc.SignInWithGoogle(context.Background(), consenting("new-gmail"))
	if err != nil {
		t.Fatal(err)
	}
	u := res.User
	if !res.Created || res.Tokens.AccessToken == "" || res.Tokens.RefreshToken == "" {
		t.Errorf("result = %+v, want a new account with a session", res)
	}
	if u.Email != "Abebe.Kebede@gmail.com" || u.Username != "abebe.kebede" || u.DisplayName != "Abebe Kebede" || u.Locale != "am" {
		t.Errorf("user = %+v", u)
	}
	if u.PasswordHash != nil || u.EmailVerifiedAt == nil || u.AdultConfirmedAt == nil || u.TermsAcceptedAt == nil {
		t.Errorf("want no password, a verified email and recorded consent: %+v", u)
	}

	// The next sign-in finds the same account through the Google subject.
	again, err := svc.SignInWithGoogle(context.Background(), GoogleSignInInput{IDToken: "new-gmail"})
	if err != nil || again.Created || again.User.ID != u.ID {
		t.Errorf("second sign-in = %+v, %v", again, err)
	}
}

// Like sign-up, a new account needs the 18+ and terms confirmations.
func TestGoogleSignInNeedsConsentForANewAccount(t *testing.T) {
	svc, users, _ := googleService(t)
	_, err := svc.SignInWithGoogle(context.Background(), GoogleSignInInput{IDToken: "new-gmail"})
	got := fieldCodes(err)
	if got["is_adult"] != "must_be_adult" || got["accept_terms"] != "must_accept_terms" {
		t.Errorf("field problems = %v", got)
	}
	if len(users.byID) != 0 {
		t.Error("an account was created without consent")
	}
}

// An existing account with a verified email gets Google linked and keeps its password.
func TestGoogleSignInLinksAVerifiedAccount(t *testing.T) {
	svc, users, sessions := googleService(t)
	ctx := context.Background()
	in := validInput()
	in.Email = "Kidist@Gmail.com"
	if _, err := svc.Signup(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyEmail(ctx, in.Email, svc.Notifier.(*fakeNotifier).codes[0]); err != nil {
		t.Fatal(err)
	}

	res, err := svc.SignInWithGoogle(ctx, GoogleSignInInput{IDToken: "kidist-gmail"}) // no consent needed
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || res.User.Username != "KidistA" || len(users.byID) != 1 {
		t.Errorf("want the existing account, got %+v", res.User)
	}
	if _, err := svc.Login(ctx, "kidista", "correct horse battery"); err != nil {
		t.Errorf("the password must still work: %v", err)
	}
	if len(sessions.revoked) != 0 {
		t.Errorf("sessions revoked: %v", sessions.revoked)
	}
}

// Pre-hijacking: someone signs up with another person's email and a password
// they know, never verifying it. When the real owner signs in with Google, that
// password and its sessions must stop working.
func TestGoogleSignInTakesBackAnUnverifiedAccount(t *testing.T) {
	svc, _, sessions := googleService(t)
	ctx := context.Background()
	in := validInput()
	in.Email = "kidist@gmail.com"
	if _, err := svc.Signup(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "kidista", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.SignInWithGoogle(ctx, GoogleSignInInput{IDToken: "kidist-gmail"})
	if err != nil {
		t.Fatal(err)
	}
	if res.User.PasswordHash != nil || res.User.EmailVerifiedAt == nil {
		t.Errorf("want no password and a verified email: %+v", res.User)
	}
	if _, err := svc.Login(ctx, "kidista", "correct horse battery"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("the squatter's password still works: %v", err)
	}
	if len(sessions.revoked) != 1 {
		t.Errorf("revoked sessions = %v, want the squatter's one", sessions.revoked)
	}
	for _, reason := range sessions.revoked {
		if reason != authdomain.RevokeGoogleLinked {
			t.Errorf("reason = %q", reason)
		}
	}
}

// The plan's "wrong audience" case: the fake fails such a token like the real
// verifier does (googleid tests check the real audience rule).
func TestGoogleSignInRejectsBadTokens(t *testing.T) {
	svc, users, _ := googleService(t)
	for _, token := range []string{"wrong-audience", "", "  "} {
		if _, err := svc.SignInWithGoogle(context.Background(), consenting(token)); !errors.Is(err, authdomain.ErrInvalidGoogleToken) {
			t.Errorf("token %q: err = %v, want ErrInvalidGoogleToken", token, err)
		}
	}
	if len(users.byID) != 0 {
		t.Error("an account was created")
	}
}

// Only emails Google is the authority for are accepted, whether or not an
// account has that email, so the answer reveals nothing.
func TestGoogleSignInNeedsAnEmailGoogleOwns(t *testing.T) {
	svc, _, _ := googleService(t)
	for _, token := range []string{"other-domain", "unverified"} {
		if _, err := svc.SignInWithGoogle(context.Background(), consenting(token)); !errors.Is(err, authdomain.ErrGoogleEmailNotOwned) {
			t.Errorf("%s: err = %v, want ErrGoogleEmailNotOwned", token, err)
		}
	}
	if _, err := svc.SignInWithGoogle(context.Background(), consenting("workspace")); err != nil {
		t.Errorf("Workspace account: %v", err)
	}
}

// A Google account keeps logging in to its account after its email changes;
// the email on the account is not touched.
func TestGoogleSignInFollowsTheSubjectNotTheEmail(t *testing.T) {
	svc, _, _ := googleService(t)
	first, err := svc.SignInWithGoogle(context.Background(), consenting("kidist-gmail"))
	if err != nil {
		t.Fatal(err)
	}
	later, err := svc.SignInWithGoogle(context.Background(), GoogleSignInInput{IDToken: "kidist-new-mail"})
	if err != nil || later.User.ID != first.User.ID || later.User.Email != "kidist@gmail.com" {
		t.Errorf("later sign-in = %+v, %v", later.User, err)
	}
}

func TestGoogleSignInConflict(t *testing.T) {
	svc, _, _ := googleService(t)
	if _, err := svc.SignInWithGoogle(context.Background(), consenting("kidist-gmail")); err != nil {
		t.Fatal(err)
	}
	// Another Google account with the same email (possible for Workspace).
	if _, err := svc.SignInWithGoogle(context.Background(), consenting("kidist-other")); !errors.Is(err, authdomain.ErrIdentityConflict) {
		t.Errorf("err = %v, want ErrIdentityConflict", err)
	}
}

func TestGoogleSignInIsOffWithoutAVerifier(t *testing.T) {
	d := testDeps(newFakeUsers(), newFakeSessions(), &fakeHasher{}, func() time.Time { return fixedNow })
	d.Google = nil
	svc, err := NewService(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SignInWithGoogle(context.Background(), consenting("new-gmail")); !errors.Is(err, authdomain.ErrGoogleUnavailable) {
		t.Errorf("err = %v, want ErrGoogleUnavailable", err)
	}
}

var validUsername = regexp.MustCompile(`^[a-z][a-z0-9_.]{2,19}$`)

func TestGeneratedUsernames(t *testing.T) {
	svc, users, _ := googleService(t)
	ctx := context.Background()
	// Taken: the generated name gets 4 random digits.
	_, _ = users.Create(ctx, usersdomain.NewUser{Email: "x@example.com", Username: "kidist"})
	for token, want := range map[string]*regexp.Regexp{
		"kidist-gmail":  regexp.MustCompile(`^kidist[0-9]{4}$`),
		"reserved-name": regexp.MustCompile(`^admin[0-9]{4}$`),
		"digits-only":   regexp.MustCompile(`^friend$`),
	} {
		res, err := svc.SignInWithGoogle(ctx, consenting(token))
		if err != nil {
			t.Fatalf("%s: %v", token, err)
		}
		if name := res.User.Username; !want.MatchString(name) || !validUsername.MatchString(name) {
			t.Errorf("%s: username %q, want %v", token, name, want)
		}
	}
}

func TestUsernameBaseAndDisplayName(t *testing.T) {
	for email, want := range map[string]string{
		"Abebe.Kebede@gmail.com":               "abebe.kebede",
		"a+tag@gmail.com":                      "atag",
		"_.9lij@gmail.com":                     "lij",
		"ab@gmail.com":                         "friend",
		"averyveryverylongemailname@gmail.com": "averyveryverylo",
		"ሰላም@gmail.com":                        "friend",
	} {
		if got := usernameBase(email); got != want || len(got) > 15 {
			t.Errorf("usernameBase(%q) = %q, want %q", email, got, want)
		}
	}
	long := "ሀ"
	for range 60 {
		long += "ለ"
	}
	if got := []rune(displayName(long, "u")); len(got) != 50 {
		t.Errorf("display name has %d letters, want 50", len(got))
	}
	if got := displayName("  ", "kidist"); got != "kidist" {
		t.Errorf("empty name gives %q", got)
	}
}
