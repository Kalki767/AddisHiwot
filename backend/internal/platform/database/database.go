// Package database creates the PostgreSQL connection pool.
package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool returns a connection pool for databaseURL. It does not wait for the
// database: connections are opened on first use, so the API can start while
// PostgreSQL is still booting and report "not ready" on /readyz until then.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// pgx errors can contain the URL, and with it the password.
		return nil, errors.New("database: cannot parse DATABASE_URL")
	}
	cfg.MaxConns = 10
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}
	return pool, nil
}
