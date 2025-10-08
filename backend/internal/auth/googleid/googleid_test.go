package googleid

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
)

const clientID = "1234-app.apps.googleusercontent.com"

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// google is a stand-in for Google's keys endpoint, serving the public half of
// keys that the tests sign with.
type google struct {
	keys    map[string]*rsa.PrivateKey
	fetches atomic.Int32
	down    atomic.Bool
}

func newGoogle(t *testing.T) (*google, *Verifier) {
	t.Helper()
	g := &google{keys: map[string]*rsa.PrivateKey{"key-1": newKey(t)}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		g.fetches.Add(1)
		if g.down.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		var set struct {
			Keys []map[string]string `json:"keys"`
		}
		for kid, k := range g.keys {
			set.Keys = append(set.Keys, map[string]string{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
				"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes())})
		}
		w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(srv.Close)
	v, err := New(Config{ClientIDs: []string{"other-client", clientID}, CertsURL: srv.URL, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return g, v
}

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": clientID, "sub": "110169484474386276334",
		"email": "kidist@gmail.com", "email_verified": true, "name": "Kidist",
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(59 * time.Minute).Unix(),
	}
}

func sign(t *testing.T, key *rsa.PrivateKey, kid string, c jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestValidTokens(t *testing.T) {
	g, v := newGoogle(t)
	for _, iss := range []string{"accounts.google.com", "https://accounts.google.com"} {
		c := validClaims()
		c["iss"] = iss
		c["hd"] = "example.org"
		id, err := v.Verify(context.Background(), sign(t, g.keys["key-1"], "key-1", c))
		if err != nil {
			t.Fatalf("iss %q: %v", iss, err)
		}
		want := domain.GoogleIdentity{Subject: "110169484474386276334", Email: "kidist@gmail.com", EmailVerified: true,
			HostedDomain: "example.org", Name: "Kidist"}
		if id != want {
			t.Errorf("identity = %+v, want %+v", id, want)
		}
	}
}

func TestRejectedTokens(t *testing.T) {
	g, v := newGoogle(t)
	key := g.keys["key-1"]
	with := func(k string, val any) jwt.MapClaims {
		c := validClaims()
		if val == nil {
			delete(c, k)
		} else {
			c[k] = val
		}
		return c
	}
	hs256 := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims())
	hs256.Header["kid"] = "key-1"
	// Algorithm confusion: an HMAC "signed" with the public key's bytes.
	confused, _ := hs256.SignedString(key.N.Bytes())
	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims()).SignedString(jwt.UnsafeAllowNoneSignatureType)

	tests := map[string]string{
		"wrong audience":        sign(t, key, "key-1", with("aud", "someone-elses-app.apps.googleusercontent.com")),
		"no audience":           sign(t, key, "key-1", with("aud", nil)),
		"wrong issuer":          sign(t, key, "key-1", with("iss", "https://evil.example")),
		"no issuer":             sign(t, key, "key-1", with("iss", nil)),
		"expired":               sign(t, key, "key-1", with("exp", now.Add(-time.Minute).Unix())),
		"no expiry":             sign(t, key, "key-1", with("exp", nil)),
		"issued in the future":  sign(t, key, "key-1", with("iat", now.Add(5*time.Minute).Unix())),
		"no subject":            sign(t, key, "key-1", with("sub", nil)),
		"no email":              sign(t, key, "key-1", with("email", nil)),
		"signed by another key": sign(t, newKey(t), "key-1", validClaims()),
		"unknown key id":        sign(t, newKey(t), "key-9", validClaims()),
		"HS256 with public key": confused,
		"alg none":              none,
		"not a JWT":             "not-a-token",
		"empty":                 "",
		"tampered payload":      sign(t, key, "key-1", validClaims()) + "x",
	}
	for name, tok := range tests {
		if _, err := v.Verify(context.Background(), tok); !errors.Is(err, domain.ErrInvalidGoogleToken) {
			t.Errorf("%s: err = %v, want ErrInvalidGoogleToken", name, err)
		}
	}
}

// Up to 30 seconds of clock difference is forgiven, no more.
func TestExpiryLeeway(t *testing.T) {
	g, v := newGoogle(t)
	c := validClaims()
	c["exp"] = now.Add(-20 * time.Second).Unix()
	if _, err := v.Verify(context.Background(), sign(t, g.keys["key-1"], "key-1", c)); err != nil {
		t.Errorf("20 s past expiry: %v", err)
	}
	c["exp"] = now.Add(-40 * time.Second).Unix()
	if _, err := v.Verify(context.Background(), sign(t, g.keys["key-1"], "key-1", c)); !errors.Is(err, domain.ErrInvalidGoogleToken) {
		t.Errorf("40 s past expiry: err = %v", err)
	}
}

func TestKeysAreCached(t *testing.T) {
	g, v := newGoogle(t)
	tok := sign(t, g.keys["key-1"], "key-1", validClaims())
	for range 5 {
		if _, err := v.Verify(context.Background(), tok); err != nil {
			t.Fatal(err)
		}
	}
	if n := g.fetches.Load(); n != 1 {
		t.Errorf("fetches = %d, want 1", n)
	}
}

// When Google rotates keys, a token with the new key id triggers one fetch;
// forged key ids cannot trigger more than one fetch a minute.
func TestRotationAndUnknownKeys(t *testing.T) {
	g, v := newGoogle(t)
	if _, err := v.Verify(context.Background(), sign(t, g.keys["key-1"], "key-1", validClaims())); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	t.Cleanup(func() { now = now.Add(-2 * time.Minute) })
	g.keys["key-2"] = newKey(t)
	if _, err := v.Verify(context.Background(), sign(t, g.keys["key-2"], "key-2", validClaims())); err != nil {
		t.Fatalf("token with the new key: %v", err)
	}
	for range 5 {
		_, _ = v.Verify(context.Background(), sign(t, g.keys["key-1"], "forged-kid", validClaims()))
	}
	if n := g.fetches.Load(); n != 2 {
		t.Errorf("fetches = %d, want 2", n)
	}
}

func TestGoogleDown(t *testing.T) {
	g, v := newGoogle(t)
	tok := sign(t, g.keys["key-1"], "key-1", validClaims())
	g.down.Store(true)
	_, err := v.Verify(context.Background(), tok)
	if err == nil || errors.Is(err, domain.ErrInvalidGoogleToken) {
		t.Fatalf("err = %v, want a fetch error (the token was never judged)", err)
	}

	// Keys fetched earlier still work while Google cannot be reached.
	g.down.Store(false)
	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	g.down.Store(true)
	now = now.Add(2 * time.Hour)
	t.Cleanup(func() { now = now.Add(-2 * time.Hour) })
	c := validClaims()
	if _, err := v.Verify(context.Background(), sign(t, g.keys["key-1"], "key-1", c)); err != nil {
		t.Errorf("cached key after its max-age with Google down: %v", err)
	}
}

func TestNewNeedsAClientID(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("New without client IDs must fail")
	}
}
