package httpx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// NewServer returns an http.Server with timeouts, so a slow or idle client
// cannot hold a connection open forever.
func NewServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second, // revisited when streaming replies arrive
		IdleTimeout:       60 * time.Second,
	}
}

// Serve runs srv on ln until ctx is cancelled (for example by SIGTERM), then
// stops accepting new connections and lets requests in flight finish, waiting
// at most shutdownTimeout. onShutdown runs first, so /readyz can report that
// the instance is draining.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, shutdownTimeout time.Duration, onShutdown func()) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return fmt.Errorf("http server stopped: %w", err)
	case <-ctx.Done():
	}

	if onShutdown != nil {
		onShutdown()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
