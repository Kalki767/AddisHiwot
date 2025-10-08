// Package googleid verifies Google ID tokens, following Google's guide
// "Verify the Google ID token on your server side" (read 26 Oct 2026):
//
//   - the signature, with Google's public keys (RS256 only);
//   - iss is accounts.google.com or https://accounts.google.com;
//   - aud is one of our OAuth client IDs;
//   - exp has not passed.
//
// The keys rotate, so they are cached for as long as Google's Cache-Control
// header allows and fetched again when a token names a key we do not have.
package googleid

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
)

// The compiler checks that Verifier keeps matching the domain interface.
var _ domain.GoogleVerifier = (*Verifier)(nil)

// CertsURL is where Google publishes its ID-token keys (JWK format).
const CertsURL = "https://www.googleapis.com/oauth2/v3/certs"

const (
	// leeway forgives small clock differences between Google and us.
	leeway = 30 * time.Second
	// defaultKeyAge is used when the keys response has no max-age.
	defaultKeyAge = time.Hour
	// refetchEvery limits fetches caused by tokens naming unknown keys, so
	// forged tokens cannot make us hammer Google.
	refetchEvery = time.Minute
)

var issuers = map[string]bool{"accounts.google.com": true, "https://accounts.google.com": true}

// Config sets up a Verifier.
type Config struct {
	ClientIDs []string         // tokens must be issued to one of these
	CertsURL  string           // default CertsURL; tests point it at a local server
	Client    *http.Client     // default: 10-second timeout
	Now       func() time.Time // default time.Now
}

// Verifier checks Google ID tokens.
type Verifier struct {
	clientIDs []string
	certsURL  string
	client    *http.Client
	now       func() time.Time

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	expires   time.Time // when the cached keys must be fetched again
	lastFetch time.Time
}

// New returns a Verifier; at least one client ID is required.
func New(c Config) (*Verifier, error) {
	if len(c.ClientIDs) == 0 {
		return nil, errors.New("googleid: at least one client ID is required")
	}
	if c.CertsURL == "" {
		c.CertsURL = CertsURL
	}
	if c.Client == nil {
		c.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return &Verifier{clientIDs: c.ClientIDs, certsURL: c.CertsURL, client: c.Client, now: c.Now}, nil
}

type claims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	HostedDomain  string `json:"hd"`
	Name          string `json:"name"`
	jwt.RegisteredClaims
}

// Verify checks the token and returns who it belongs to. Every rejected token
// gives domain.ErrInvalidGoogleToken; failing to fetch Google's keys gives
// another error, because then the token was not judged at all.
func (v *Verifier) Verify(ctx context.Context, idToken string) (domain.GoogleIdentity, error) {
	var fetchErr error
	var c claims
	_, err := jwt.ParseWithClaims(idToken, &c, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		key, err := v.key(ctx, kid)
		if err != nil && !errors.Is(err, errUnknownKey) {
			fetchErr = err
		}
		return key, err
	},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(v.clientIDs...),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(leeway),
		jwt.WithTimeFunc(v.now),
	)
	if fetchErr != nil {
		return domain.GoogleIdentity{}, fetchErr
	}
	if err != nil {
		return domain.GoogleIdentity{}, fmt.Errorf("%w: %w", domain.ErrInvalidGoogleToken, err)
	}
	// jwt.WithIssuer accepts one value; Google uses two spellings.
	if !issuers[c.Issuer] || c.Subject == "" || c.Email == "" {
		return domain.GoogleIdentity{}, fmt.Errorf("%w: issuer %q, subject or email missing", domain.ErrInvalidGoogleToken, c.Issuer)
	}
	return domain.GoogleIdentity{Subject: c.Subject, Email: c.Email, EmailVerified: c.EmailVerified,
		HostedDomain: c.HostedDomain, Name: c.Name}, nil
}

var errUnknownKey = errors.New("googleid: unknown signing key")

// key returns the public key with id kid, fetching Google's keys when the
// cache has expired or does not know kid (at most once per refetchEvery).
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.now()
	if k, ok := v.keys[kid]; ok && now.Before(v.expires) {
		return k, nil
	}
	if now.Before(v.expires) && now.Sub(v.lastFetch) < refetchEvery {
		return nil, errUnknownKey
	}
	keys, maxAge, err := v.fetch(ctx)
	v.lastFetch = now
	if err != nil {
		// Google keeps old keys published for a while, so a cached key is
		// still trusted when a fetch fails rather than failing every sign-in.
		if k, ok := v.keys[kid]; ok {
			return k, nil
		}
		return nil, err
	}
	v.keys, v.expires = keys, now.Add(maxAge)
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, errUnknownKey
}

var maxAgePattern = regexp.MustCompile(`max-age=(\d+)`)

func (v *Verifier) fetch(ctx context.Context) (map[string]*rsa.PublicKey, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("googleid: keys request: %w", err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("googleid: fetch keys: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("googleid: fetch keys: status %d", resp.StatusCode)
	}
	var set struct {
		Keys []struct {
			Kty, Kid, N, E string
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&set); err != nil {
		return nil, 0, fmt.Errorf("googleid: decode keys: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := rsaKey(k.N, k.E)
		if err != nil {
			return nil, 0, fmt.Errorf("googleid: key %s: %w", k.Kid, err)
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, 0, errors.New("googleid: no RSA keys in the response")
	}
	maxAge := defaultKeyAge
	if m := maxAgePattern.FindStringSubmatch(resp.Header.Get("Cache-Control")); m != nil {
		if s, err := strconv.Atoi(m[1]); err == nil && s > 0 {
			maxAge = time.Duration(s) * time.Second
		}
	}
	return keys, maxAge, nil
}

func rsaKey(n64, e64 string) (*rsa.PublicKey, error) {
	n, err := base64.RawURLEncoding.DecodeString(n64)
	if err != nil {
		return nil, fmt.Errorf("modulus: %w", err)
	}
	e, err := base64.RawURLEncoding.DecodeString(e64)
	if err != nil || len(e) == 0 || len(e) > 4 {
		return nil, errors.New("bad exponent")
	}
	if len(n) < 256 { // RSA-2048 at least
		return nil, fmt.Errorf("modulus of %d bits is too small", len(n)*8)
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}, nil
}
