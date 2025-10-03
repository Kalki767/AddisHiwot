package argon2id

import (
	"errors"
	"strings"
	"testing"
)

// Tests use cheap parameters so they run fast; one test checks the real ones.
var cheap = Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

func TestHashAndVerify(t *testing.T) {
	h := New(cheap)
	encoded, err := h.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := h.Verify("correct horse battery", encoded); !ok || err != nil {
		t.Errorf("right password: ok=%t err=%v", ok, err)
	}
	if ok, err := h.Verify("correct horse batterY", encoded); ok || err != nil {
		t.Errorf("wrong password: ok=%t err=%v", ok, err)
	}
}

func TestAmharicPasswords(t *testing.T) {
	h := New(cheap)
	encoded, _ := h.Hash("ሰላም ለኢትዮጵያ")
	if ok, _ := h.Verify("ሰላም ለኢትዮጵያ", encoded); !ok {
		t.Error("an Amharic password must verify")
	}
	if ok, _ := h.Verify("ሰላም ለኢትዮጵ", encoded); ok {
		t.Error("a different Amharic password must not verify")
	}
}

func TestEachHashHasItsOwnSalt(t *testing.T) {
	h := New(cheap)
	a, _ := h.Hash("same password")
	b, _ := h.Hash("same password")
	if a == b {
		t.Error("two hashes of the same password are equal: the salt is not random")
	}
}

func TestFormatUsesOWASPParameters(t *testing.T) {
	encoded, err := New(OWASPMinimum).Hash("x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash %q does not carry the OWASP minimum parameters", encoded)
	}
}

// A hash made with old parameters still verifies after the parameters change.
func TestVerifyUsesTheParametersInTheHash(t *testing.T) {
	old, _ := New(cheap).Hash("pw-before-upgrade")
	newer := New(Params{MemoryKiB: 128, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	if ok, err := newer.Verify("pw-before-upgrade", old); !ok || err != nil {
		t.Errorf("old hash: ok=%t err=%v", ok, err)
	}
	if !newer.NeedsRehash(old) {
		t.Error("NeedsRehash = false for a hash with older parameters")
	}
	fresh, _ := newer.Hash("x")
	if newer.NeedsRehash(fresh) {
		t.Error("NeedsRehash = true for a hash with current parameters")
	}
}

func TestMalformedHashes(t *testing.T) {
	good, _ := New(cheap).Hash("pw")
	parts := strings.Split(good, "$")
	bad := map[string]string{
		"empty":           "",
		"bcrypt":          "$2a$10$abcdefghijklmnopqrstuuabcdefghijklmnopqrstuvwxyz01234",
		"argon2i":         strings.Replace(good, "argon2id", "argon2i", 1),
		"wrong version":   strings.Replace(good, "v=19", "v=16", 1),
		"bad params":      strings.Replace(good, parts[3], "m=x,t=1,p=1", 1),
		"huge memory":     strings.Replace(good, parts[3], "m=99999999,t=1,p=1", 1),
		"zero iterations": strings.Replace(good, parts[3], "m=64,t=0,p=1", 1),
		"salt not base64": strings.Replace(good, parts[4], "!!!", 1),
		"short key":       strings.Replace(good, parts[5], "AAAA", 1),
		"huge salt":       strings.Replace(good, parts[4], strings.Repeat("A", 200), 1),
		"too many fields": good + "$extra",
	}
	for name, s := range bad {
		if ok, err := New(cheap).Verify("pw", s); ok || !errors.Is(err, ErrMalformedHash) {
			t.Errorf("%s: ok=%t err=%v, want ErrMalformedHash", name, ok, err)
		}
	}
}

// BenchmarkHashOWASPMinimum measures one hash with the production parameters
// (go test -bench=. ./internal/auth/argon2id/).
func BenchmarkHashOWASPMinimum(b *testing.B) {
	h := New(OWASPMinimum)
	for b.Loop() {
		if _, err := h.Hash("correct horse battery"); err != nil {
			b.Fatal(err)
		}
	}
}
