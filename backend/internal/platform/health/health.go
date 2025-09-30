// Package health serves the liveness and readiness endpoints.
//
//	/healthz  "is the process alive?"  always 200 while it can answer at all
//	/readyz   "can it serve requests?"  200 only when the database answers and
//	          the instance is not shutting down, otherwise 503
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// Pinger is anything that can check its connection, such as *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Checker answers the health endpoints.
type Checker struct {
	db       Pinger
	timeout  time.Duration
	logger   *slog.Logger
	draining atomic.Bool
}

// New returns a Checker that gives the database at most timeout to answer.
func New(db Pinger, timeout time.Duration, logger *slog.Logger) *Checker {
	return &Checker{db: db, timeout: timeout, logger: logger}
}

// StartDraining makes /readyz fail from now on, so traffic moves away while
// requests in flight finish during a graceful shutdown.
func (c *Checker) StartDraining() { c.draining.Store(true) }

// Live handles GET /healthz.
func (c *Checker) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready handles GET /readyz.
func (c *Checker) Ready(w http.ResponseWriter, r *http.Request) {
	if c.draining.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "shutting_down"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), c.timeout)
	defer cancel()
	if err := c.db.Ping(ctx); err != nil {
		// The reason goes to the log only; callers just learn which part is down.
		c.logger.WarnContext(r.Context(), "readiness check failed", slog.String("check", "database"), slog.Any("error", err))
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "database": "down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "database": "up"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
