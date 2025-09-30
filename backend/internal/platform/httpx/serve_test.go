package httpx

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// A request that is running when shutdown starts must still finish, and the
// server must stop accepting new connections.
func TestServeFinishesInFlightRequestOnShutdown(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("finished"))
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	ctx, stop := context.WithCancel(context.Background())
	var draining atomic.Bool
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, srv, ln, 5*time.Second, func() { draining.Store(true) }) }()

	body := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			body <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		body <- string(b)
	}()

	<-started
	stop() // like SIGTERM while the request is running
	waitFor(t, draining.Load)

	// The listener closes right after onShutdown; new connections must then be refused.
	waitFor(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return false
		}
		return true
	})

	close(release)
	if got := <-body; got != "finished" {
		t.Errorf("in-flight request got %q, want it to finish", got)
	}
	if err := <-served; err != nil {
		t.Errorf("Serve returned %v, want nil", err)
	}
}

func TestServeGivesUpAfterShutdownTimeout(t *testing.T) {
	started := make(chan struct{})
	srv := NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done() // never finishes on its own
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, srv, ln, 100*time.Millisecond, nil) }()
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err == nil {
			resp.Body.Close()
		}
	}()

	<-started
	stop()
	select {
	case err := <-served:
		if err == nil {
			t.Error("Serve returned nil although a request was still running at the deadline")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the shutdown timeout")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
