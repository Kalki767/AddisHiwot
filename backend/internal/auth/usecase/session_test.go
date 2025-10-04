package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
)

// fakeSessions is an in-memory SessionRepository with the same rules as the
// PostgreSQL one.
type fakeSessions struct {
	mu        sync.Mutex
	tokens    map[string]*fakeToken // by token hash
	revoked   map[uuid.UUID]authdomain.RevokeReason
	raceLoser bool // makes the next Rotate behave as if another request won
}

type fakeToken struct {
	id, session, user uuid.UUID
	expires           time.Time
	used              bool
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{tokens: map[string]*fakeToken{}, revoked: map[uuid.UUID]authdomain.RevokeReason{}}
}

func (f *fakeSessions) CreateSession(_ context.Context, userID uuid.UUID, hash []byte, exp time.Time) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sid := uuid.New()
	f.tokens[string(hash)] = &fakeToken{id: uuid.New(), session: sid, user: userID, expires: exp}
	return sid, nil
}

func (f *fakeSessions) GetRefreshToken(_ context.Context, hash []byte) (authdomain.RefreshTokenRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokens[string(hash)]
	if !ok {
		return authdomain.RefreshTokenRecord{}, authdomain.ErrInvalidRefreshToken
	}
	_, revoked := f.revoked[t.session]
	return authdomain.RefreshTokenRecord{ID: t.id, SessionID: t.session, UserID: t.user, ExpiresAt: t.expires,
		Used: t.used, SessionRevoked: revoked}, nil
}

func (f *fakeSessions) Rotate(_ context.Context, oldID, sid uuid.UUID, newHash []byte, exp time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.raceLoser {
		return authdomain.ErrRefreshTokenReused
	}
	for _, t := range f.tokens {
		if t.id == oldID {
			if t.used {
				return authdomain.ErrRefreshTokenReused
			}
			t.used = true
			f.tokens[string(newHash)] = &fakeToken{id: uuid.New(), session: sid, user: t.user, expires: exp}
			return nil
		}
	}
	return authdomain.ErrInvalidRefreshToken
}

func (f *fakeSessions) RevokeSession(_ context.Context, sid uuid.UUID, reason authdomain.RevokeReason) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, done := f.revoked[sid]; !done {
		f.revoked[sid] = reason
	}
	return nil
}

// fakeTokens issues readable tokens "access|user|session|expiry-unix".
type fakeTokens struct{}

func (fakeTokens) Issue(user, session uuid.UUID, now time.Time) (string, time.Time, error) {
	exp := now.Add(15 * time.Minute)
	return fmt.Sprintf("access|%s|%s|%d", user, session, exp.Unix()), exp, nil
}

func (fakeTokens) Verify(token string, now time.Time) (authdomain.Claims, error) {
	var user, session string
	var exp int64
	parts := strings.Split(token, "|")
	if len(parts) != 4 || parts[0] != "access" {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	user, session = parts[1], parts[2]
	if _, err := fmt.Sscan(parts[3], &exp); err != nil {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	if !now.Before(time.Unix(exp, 0)) {
		return authdomain.Claims{}, authdomain.ErrTokenExpired
	}
	return authdomain.Claims{UserID: uuid.MustParse(user), SessionID: uuid.MustParse(session), ExpiresAt: time.Unix(exp, 0)}, nil
}

// sessionService returns a service with one signed-up user and a clock the test can move.
func sessionService(t *testing.T) (*Service, *fakeSessions, *time.Time) {
	t.Helper()
	now := fixedNow
	sessions := newFakeSessions()
	svc, err := NewService(Deps{Users: newFakeUsers(), Sessions: sessions, Hasher: &fakeHasher{}, Tokens: fakeTokens{},
		Now: func() time.Time { return now }, RefreshTTL: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Signup(context.Background(), validInput()); err != nil {
		t.Fatal(err)
	}
	return svc, sessions, &now
}

func login(t *testing.T, svc *Service) LoginResult {
	t.Helper()
	res, err := svc.Login(context.Background(), "kidista", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestLoginStartsASession(t *testing.T) {
	svc, _, _ := sessionService(t)
	res := login(t, svc)
	if res.Tokens.AccessToken == "" || res.Tokens.RefreshToken == "" {
		t.Fatalf("tokens = %+v", res.Tokens)
	}
	if !res.Tokens.AccessExpiresAt.Equal(fixedNow.Add(15*time.Minute)) || !res.Tokens.RefreshExpiresAt.Equal(fixedNow.Add(30*24*time.Hour)) {
		t.Errorf("expiries = %v, %v", res.Tokens.AccessExpiresAt, res.Tokens.RefreshExpiresAt)
	}
	p, err := svc.Authenticate(res.Tokens.AccessToken)
	if err != nil || p.UserID != res.User.ID {
		t.Errorf("Authenticate = %+v, %v", p, err)
	}
}

// Plan check: rotation works.
func TestRefreshRotates(t *testing.T) {
	svc, sessions, _ := sessionService(t)
	first := login(t, svc).Tokens
	second, err := svc.Refresh(context.Background(), first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if second.RefreshToken == first.RefreshToken || second.AccessToken == "" {
		t.Error("refresh must hand out a new refresh token")
	}
	third, err := svc.Refresh(context.Background(), second.RefreshToken)
	if err != nil || third.RefreshToken == second.RefreshToken {
		t.Errorf("second rotation: %v", err)
	}
	if len(sessions.revoked) != 0 {
		t.Error("normal rotation must not revoke anything")
	}
}

// Plan check: reusing an old refresh token revokes the whole session family.
func TestReusingAnOldRefreshTokenRevokesTheSession(t *testing.T) {
	svc, sessions, _ := sessionService(t)
	stolen := login(t, svc).Tokens
	current, err := svc.Refresh(context.Background(), stolen.RefreshToken) // the owner refreshes
	if err != nil {
		t.Fatal(err)
	}

	// The thief tries the copied (already used) token.
	if _, err := svc.Refresh(context.Background(), stolen.RefreshToken); !errors.Is(err, authdomain.ErrRefreshTokenReused) {
		t.Fatalf("reuse: err = %v, want ErrRefreshTokenReused", err)
	}
	if len(sessions.revoked) != 1 {
		t.Fatalf("revoked sessions = %d, want 1", len(sessions.revoked))
	}
	for _, reason := range sessions.revoked {
		if reason != authdomain.RevokeReuseDetected {
			t.Errorf("reason = %q", reason)
		}
	}
	// The newest token of the family is dead too: everyone must log in again.
	if _, err := svc.Refresh(context.Background(), current.RefreshToken); !errors.Is(err, authdomain.ErrInvalidRefreshToken) {
		t.Errorf("newest token after reuse: err = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestLosingARotationRaceRevokesTheSession(t *testing.T) {
	svc, sessions, _ := sessionService(t)
	tokens := login(t, svc).Tokens
	sessions.raceLoser = true
	if _, err := svc.Refresh(context.Background(), tokens.RefreshToken); !errors.Is(err, authdomain.ErrRefreshTokenReused) {
		t.Errorf("err = %v, want ErrRefreshTokenReused", err)
	}
	if len(sessions.revoked) != 1 {
		t.Error("a lost race means the token was used twice: the session must be revoked")
	}
}

func TestExpiredRefreshTokenIsInvalid(t *testing.T) {
	svc, _, now := sessionService(t)
	tokens := login(t, svc).Tokens
	*now = now.Add(30*24*time.Hour + time.Second)
	if _, err := svc.Refresh(context.Background(), tokens.RefreshToken); !errors.Is(err, authdomain.ErrInvalidRefreshToken) {
		t.Errorf("err = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestUnknownOrEmptyRefreshTokenIsInvalid(t *testing.T) {
	svc, _, _ := sessionService(t)
	for _, tok := range []string{"", "made-up"} {
		if _, err := svc.Refresh(context.Background(), tok); !errors.Is(err, authdomain.ErrInvalidRefreshToken) {
			t.Errorf("Refresh(%q) err = %v", tok, err)
		}
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	svc, sessions, _ := sessionService(t)
	tokens := login(t, svc).Tokens
	for range 2 { // logging out twice is fine
		if err := svc.Logout(context.Background(), tokens.RefreshToken); err != nil {
			t.Fatal(err)
		}
	}
	for _, reason := range sessions.revoked {
		if reason != authdomain.RevokeLogout {
			t.Errorf("reason = %q", reason)
		}
	}
	if _, err := svc.Refresh(context.Background(), tokens.RefreshToken); !errors.Is(err, authdomain.ErrInvalidRefreshToken) {
		t.Errorf("refresh after logout: err = %v", err)
	}
	if err := svc.Logout(context.Background(), "never-issued"); err != nil {
		t.Errorf("logout with an unknown token: %v, want nil", err)
	}
}

// Plan check (use-case side): an expired access token is reported as expired.
func TestAuthenticateExpiredAccessToken(t *testing.T) {
	svc, _, now := sessionService(t)
	tokens := login(t, svc).Tokens
	*now = now.Add(15 * time.Minute)
	if _, err := svc.Authenticate(tokens.AccessToken); !errors.Is(err, authdomain.ErrTokenExpired) {
		t.Errorf("err = %v, want ErrTokenExpired", err)
	}
}

func TestNewServiceRequiresEveryDependency(t *testing.T) {
	if _, err := NewService(Deps{Users: newFakeUsers()}); err == nil {
		t.Error("missing dependencies were accepted")
	}
}
