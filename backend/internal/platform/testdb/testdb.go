// Package testdb gives each test its own fresh, fully migrated PostgreSQL database.
//
// The first call in a test run builds a template database with every migration
// applied. Each test then gets a copy of it (CREATE DATABASE ... TEMPLATE), which
// is much faster than migrating per test, and the copy is dropped when the test
// ends. The template name includes a hash of the migration files, so editing or
// adding a migration builds a new template automatically.
//
// TEST_DATABASE_URL points at a maintenance database on the server, for example
// postgres://user:pass@127.0.0.1:5433/postgres; the user must be allowed to
// create databases. Without it the test is skipped, unless TEST_REQUIRE_DB=1
// (set by `make test`), in which case it fails, so a database test never skips
// unnoticed.
package testdb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/database"
	"github.com/Kalki767/AddisHiwot/backend/migrations"
)

// lockKey serialises template building and copying across parallel test processes.
const lockKey = 7_316_220_061

// New returns a pool connected to a new database that only this test uses.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		if os.Getenv("TEST_REQUIRE_DB") == "1" {
			t.Fatal("TEST_DATABASE_URL is not set; start the stack with `make up`")
		}
		t.Skip("TEST_DATABASE_URL not set: skipping database test (run `make test`)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	name := "addishiwot_test_" + randomHex(8)
	if err := createFromTemplate(ctx, adminURL, name); err != nil {
		t.Fatalf("testdb: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := exec(ctx, adminURL, "DROP DATABASE IF EXISTS "+ident(name)+" WITH (FORCE)"); err != nil {
			t.Errorf("testdb: drop %s: %v", name, err)
		}
	})

	pool, err := database.NewPool(ctx, withDatabase(adminURL, name))
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// NewEmptyURL creates a database with no migrations applied, dropped when the
// test ends, and returns its URL. For testing the migrations themselves.
func NewEmptyURL(t testing.TB) string {
	t.Helper()
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		if os.Getenv("TEST_REQUIRE_DB") == "1" {
			t.Fatal("TEST_DATABASE_URL is not set; start the stack with `make up`")
		}
		t.Skip("TEST_DATABASE_URL not set: skipping database test (run `make test`)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name := "addishiwot_test_" + randomHex(8)
	if err := exec(ctx, adminURL, "CREATE DATABASE "+ident(name)); err != nil {
		t.Fatalf("testdb: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := exec(ctx, adminURL, "DROP DATABASE IF EXISTS "+ident(name)+" WITH (FORCE)"); err != nil {
			t.Errorf("testdb: drop %s: %v", name, err)
		}
	})
	return withDatabase(adminURL, name)
}

func createFromTemplate(ctx context.Context, adminURL, name string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect to TEST_DATABASE_URL: %w", err)
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey) //nolint:errcheck // the lock also ends with the connection

	tmpl, err := templateName()
	if err != nil {
		return err
	}
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", tmpl).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if err := buildTemplate(ctx, conn, adminURL, tmpl); err != nil {
			return err
		}
	}
	_, err = conn.Exec(ctx, "CREATE DATABASE "+ident(name)+" TEMPLATE "+ident(tmpl))
	return err
}

// buildTemplate migrates a database under a temporary name and renames it only
// when every migration succeeded, so a failed migration never leaves a broken template.
func buildTemplate(ctx context.Context, conn *pgx.Conn, adminURL, tmpl string) error {
	building := tmpl + "_building"
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+ident(building)+" WITH (FORCE)"); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+ident(building)); err != nil {
		return err
	}
	m, err := database.NewMigrator(withDatabase(adminURL, building))
	if err != nil {
		return err
	}
	upErr := m.Up()
	if err := m.Close(); err != nil && upErr == nil {
		upErr = err
	}
	if upErr != nil {
		return upErr
	}
	if _, err := conn.Exec(ctx, "ALTER DATABASE "+ident(building)+" RENAME TO "+ident(tmpl)); err != nil {
		return err
	}
	return dropOldTemplates(ctx, conn, tmpl)
}

// dropOldTemplates removes templates built from earlier versions of the
// migrations, so they do not pile up as migrations are added.
func dropOldTemplates(ctx context.Context, conn *pgx.Conn, keep string) error {
	rows, err := conn.Query(ctx, `SELECT datname FROM pg_database WHERE datname LIKE 'addishiwot\_tmpl\_%' AND datname <> $1`, keep)
	if err != nil {
		return err
	}
	old, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, name := range old {
		if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+ident(name)+" WITH (FORCE)"); err != nil {
			return err
		}
	}
	return nil
}

// templateName is derived from the content of every migration file.
func templateName() (string, error) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		b, err := fs.ReadFile(migrations.FS, n)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", n, len(b))
		h.Write(b)
	}
	return "addishiwot_tmpl_" + hex.EncodeToString(h.Sum(nil))[:12], nil
}

func exec(ctx context.Context, adminURL, sql string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	_, err = conn.Exec(ctx, sql)
	return err
}

func withDatabase(rawURL, name string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.Path = "/" + name
	return u.String()
}

func ident(name string) string { return pgx.Identifier{name}.Sanitize() }

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
