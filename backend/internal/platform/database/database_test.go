package database

import (
	"context"
	"strings"
	"testing"
)

func TestNewPoolDoesNotConnectYet(t *testing.T) {
	// Nothing listens on port 1; creating the pool must still succeed.
	pool, err := NewPool(context.Background(), "postgres://u:p@127.0.0.1:1/db?sslmode=disable")
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	pool.Close()
}

func TestNewPoolErrorHidesThePassword(t *testing.T) {
	_, err := NewPool(context.Background(), "postgres://u:secret-pw@127.0.0.1:5432/db?sslmode=bogus")
	if err == nil {
		t.Fatal("expected an error for an invalid sslmode")
	}
	if strings.Contains(err.Error(), "secret-pw") {
		t.Errorf("error leaks the password: %v", err)
	}
}
