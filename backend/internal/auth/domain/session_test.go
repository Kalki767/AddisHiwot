package domain

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestNewRefreshToken(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		token, hash := NewRefreshToken()
		if len(token) != 43 { // 32 bytes in unpadded base64url
			t.Fatalf("token %q has %d characters, want 43", token, len(token))
		}
		if seen[token] {
			t.Fatal("the same refresh token was generated twice")
		}
		seen[token] = true
		if len(hash) != 32 || !bytes.Equal(hash, HashRefreshToken(token)) {
			t.Fatal("hash must be the 32-byte SHA-256 of the token")
		}
	}
}

func TestHashRefreshTokenDiffersPerToken(t *testing.T) {
	if bytes.Equal(HashRefreshToken("a"), HashRefreshToken("b")) {
		t.Error("different tokens gave the same hash")
	}
}

func TestPrincipalContext(t *testing.T) {
	if _, ok := PrincipalFrom(context.Background()); ok {
		t.Error("an empty context must have no principal")
	}
	p := Principal{UserID: uuid.New(), SessionID: uuid.New()}
	got, ok := PrincipalFrom(WithPrincipal(context.Background(), p))
	if !ok || got != p {
		t.Errorf("got %+v, %t", got, ok)
	}
}
