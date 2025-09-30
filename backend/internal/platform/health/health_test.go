package health

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/database"
)

type fakeDB struct {
	err   error
	delay time.Duration
}

func (f fakeDB) Ping(ctx context.Context) error {
	select {
	case <-time.After(f.delay):
		return f.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

var quiet = slog.New(slog.DiscardHandler)

func get(t *testing.T, h http.HandlerFunc) (int, map[string]string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	return rec.Code, body
}

func TestLiveIsAlways200(t *testing.T) {
	c := New(fakeDB{err: errors.New("down")}, time.Second, quiet)
	if code, body := get(t, c.Live); code != http.StatusOK || body["status"] != "ok" {
		t.Errorf("Live = %d %v", code, body)
	}
}

func TestReadyWhenDatabaseAnswers(t *testing.T) {
	c := New(fakeDB{}, time.Second, quiet)
	if code, body := get(t, c.Ready); code != http.StatusOK || body["database"] != "up" {
		t.Errorf("Ready = %d %v", code, body)
	}
}

func TestReadyIs503WhenDatabaseFails(t *testing.T) {
	c := New(fakeDB{err: errors.New("connection refused")}, time.Second, quiet)
	code, body := get(t, c.Ready)
	if code != http.StatusServiceUnavailable || body["database"] != "down" {
		t.Errorf("Ready = %d %v", code, body)
	}
	if _, leaked := body["error"]; leaked {
		t.Error("error details must stay in the log, not the response")
	}
}

func TestReadyIs503WhenDatabaseIsTooSlow(t *testing.T) {
	c := New(fakeDB{delay: time.Second}, 50*time.Millisecond, quiet)
	start := time.Now()
	code, _ := get(t, c.Ready)
	if code != http.StatusServiceUnavailable {
		t.Errorf("Ready = %d, want 503", code)
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Errorf("Ready waited %v, want it to give up after the 50ms timeout", took)
	}
}

func TestReadyIs503WhileDraining(t *testing.T) {
	c := New(fakeDB{}, time.Second, quiet)
	c.StartDraining()
	if code, body := get(t, c.Ready); code != http.StatusServiceUnavailable || body["status"] != "shutting_down" {
		t.Errorf("Ready = %d %v", code, body)
	}
	if code, _ := get(t, c.Live); code != http.StatusOK {
		t.Errorf("Live = %d while draining, want 200", code)
	}
}

// With the real PostgreSQL driver, a database that is not running (nothing
// listening on the port) must make /readyz answer 503.
func TestReadyIs503WhenPostgresIsStopped(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // the port is now free: like a stopped PostgreSQL

	pool, err := database.NewPool(context.Background(), "postgres://u:p@"+addr+"/db?sslmode=disable")
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	c := New(pool, 2*time.Second, quiet)
	if code, body := get(t, c.Ready); code != http.StatusServiceUnavailable || body["database"] != "down" {
		t.Errorf("Ready = %d %v, want 503 with database down", code, body)
	}
}
