package database

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/Kalki767/AddisHiwot/backend/migrations"
)

// Migrator applies the embedded migrations to one database.
type Migrator struct {
	m *migrate.Migrate
}

// NewMigrator connects to databaseURL (postgres:// or postgresql://).
// golang-migrate takes a lock in the database, so two migrators never run at once.
func NewMigrator(databaseURL string) (*Migrator, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("migrations: open embedded files: %w", err)
	}
	url, err := migrateURL(databaseURL)
	if err != nil {
		return nil, err
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, url)
	if err != nil {
		return nil, errors.New("migrations: cannot connect to the database")
	}
	return &Migrator{m: m}, nil
}

// Up applies every migration not applied yet. Nothing to do is not an error.
func (mg *Migrator) Up() error {
	if err := mg.m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: up: %w", err)
	}
	return nil
}

// Down rolls back the given number of migrations (steps >= 1).
func (mg *Migrator) Down(steps int) error {
	if steps < 1 {
		return fmt.Errorf("migrations: down needs at least 1 step, got %d", steps)
	}
	if err := mg.m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: down %d: %w", steps, err)
	}
	return nil
}

// DownAll rolls back every migration.
func (mg *Migrator) DownAll() error {
	if err := mg.m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: down all: %w", err)
	}
	return nil
}

// Version reports the current migration version (0 when none is applied) and
// whether the last migration failed half-way ("dirty").
func (mg *Migrator) Version() (version uint, dirty bool, err error) {
	v, d, err := mg.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	return v, d, err
}

// Force records version as applied without running anything. Only for repairing
// a dirty state by hand after fixing the database.
func (mg *Migrator) Force(version int) error {
	return mg.m.Force(version)
}

// Close releases the database connection.
func (mg *Migrator) Close() error {
	srcErr, dbErr := mg.m.Close()
	return errors.Join(srcErr, dbErr)
}

// migrateURL turns a postgres:// URL into the pgx5:// form golang-migrate uses
// for the pgx v5 driver.
func migrateURL(databaseURL string) (string, error) {
	for _, prefix := range []string{"postgres://", "postgresql://"} {
		if rest, ok := strings.CutPrefix(databaseURL, prefix); ok {
			return "pgx5://" + rest, nil
		}
	}
	return "", errors.New("migrations: DATABASE_URL must start with postgres://")
}
