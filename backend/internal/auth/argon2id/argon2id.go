// Package argon2id hashes passwords with Argon2id, the algorithm the OWASP
// Password Storage Cheat Sheet recommends first.
//
// Hashes are stored in the standard PHC string format, which carries its own
// parameters and salt:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<salt, base64>$<hash, base64>
//
// so parameters can be raised later without breaking existing hashes.
package argon2id

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
)

// The compiler checks that Hasher keeps matching the domain interface.
var _ domain.PasswordHasher = (*Hasher)(nil)

// Params are the Argon2id cost settings.
type Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// OWASPMinimum is the first minimum in the OWASP Password Storage Cheat Sheet
// (read 25 Oct 2026): 19 MiB of memory, 2 iterations, 1 degree of parallelism.
// Salt and key are the common 16 and 32 bytes.
var OWASPMinimum = Params{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}

// ErrMalformedHash means a stored hash is not a valid Argon2id PHC string.
var ErrMalformedHash = errors.New("argon2id: malformed hash")

// Hasher hashes new passwords with its Params and verifies hashes made with any params.
type Hasher struct {
	params Params
}

// New returns a Hasher using p for new hashes.
func New(p Params) *Hasher { return &Hasher{params: p} }

// Hash returns the PHC string for password with a new random salt.
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("argon2id: salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, h.params.KeyLength)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify reports whether password matches encodedHash, using the parameters
// stored in the hash. The comparison takes the same time however many bytes match.
func (h *Hasher) Verify(password, encodedHash string) (bool, error) {
	p, salt, key, err := decode(encodedHash)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, p.KeyLength)
	return subtle.ConstantTimeCompare(candidate, key) == 1, nil
}

// NeedsRehash reports whether encodedHash was made with other parameters than
// this Hasher's, so it can be re-hashed after the next successful login.
func (h *Hasher) NeedsRehash(encodedHash string) bool {
	p, _, _, err := decode(encodedHash)
	return err != nil || p != h.params
}

// maxBytes caps salt and key length in a stored hash; real ones are 16 and 32.
const maxBytes = 64

func decode(s string) (p Params, salt, key []byte, err error) {
	parts := strings.Split(s, "$") // "", "argon2id", "v=19", "m=…,t=…,p=…", salt, key
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, ErrMalformedHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Iterations, &p.Parallelism); err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}
	// Refuse absurd costs from a corrupted or planted hash (max 1 GiB, 100 passes, 16 lanes).
	if p.MemoryKiB == 0 || p.MemoryKiB > 1<<20 || p.Iterations == 0 || p.Iterations > 100 || p.Parallelism == 0 || p.Parallelism > 16 {
		return Params{}, nil, nil, ErrMalformedHash
	}
	b64 := base64.RawStdEncoding
	if salt, err = b64.DecodeString(parts[4]); err != nil || len(salt) < 8 || len(salt) > maxBytes {
		return Params{}, nil, nil, ErrMalformedHash
	}
	if key, err = b64.DecodeString(parts[5]); err != nil || len(key) < 16 || len(key) > maxBytes {
		return Params{}, nil, nil, ErrMalformedHash
	}
	// Both lengths were checked to be at most maxBytes just above.
	p.SaltLength = uint32(len(salt)) //nolint:gosec // G115: 8 to 64
	p.KeyLength = uint32(len(key))   //nolint:gosec // G115: 16 to 64
	return p, salt, key, nil
}
