package database_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/database"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/testdb"
)

// Up → down all → up must work, leave nothing behind after down, and rebuild
// exactly the same schema the second time.
func TestMigrationsUpDownUp(t *testing.T) {
	url := testdb.NewEmptyURL(t)
	m, err := database.NewMigrator(url)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if err := m.Up(); err != nil {
		t.Fatalf("first up: %v", err)
	}
	first := schema(t, url)
	if !strings.Contains(first, "table users") {
		t.Fatalf("users table missing after up:\n%s", first)
	}
	assertVersion(t, m, 1)

	if err := m.Up(); err != nil {
		t.Fatalf("up with nothing to do must not fail: %v", err)
	}

	if err := m.DownAll(); err != nil {
		t.Fatalf("down: %v", err)
	}
	if left := schema(t, url); left != "" {
		t.Errorf("down left objects behind:\n%s", left)
	}
	assertVersion(t, m, 0)

	if err := m.Up(); err != nil {
		t.Fatalf("second up: %v", err)
	}
	if second := schema(t, url); second != first {
		t.Errorf("schema differs after up → down → up\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestMigratorRejectsNonPostgresURL(t *testing.T) {
	if _, err := database.NewMigrator("mysql://u:secret@h/db"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("err = %v, want an error without the password", err)
	}
}

func assertVersion(t *testing.T, m *database.Migrator, want uint) {
	t.Helper()
	v, dirty, err := m.Version()
	if err != nil || v != want || dirty {
		t.Errorf("version = %d dirty=%t err=%v, want %d clean", v, dirty, err, want)
	}
}

// schema lists our tables with their columns, functions, triggers, constraints,
// indexes and extensions (the migration bookkeeping table is left out).
func schema(t *testing.T, url string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `
		SELECT 'table ' || table_name || '.' || column_name || ' ' || data_type || ' null=' || is_nullable || ' default=' || coalesce(column_default, '')
		  FROM information_schema.columns WHERE table_schema = 'public' AND table_name <> 'schema_migrations'
		UNION ALL SELECT 'table ' || tablename FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'schema_migrations'
		UNION ALL SELECT 'function ' || proname FROM pg_proc WHERE pronamespace = 'public'::regnamespace
		UNION ALL SELECT 'trigger ' || tgname FROM pg_trigger WHERE NOT tgisinternal
		UNION ALL SELECT 'constraint ' || conname || ' ' || pg_get_constraintdef(oid) FROM pg_constraint WHERE connamespace = 'public'::regnamespace AND conrelid::regclass::text <> 'schema_migrations'
		UNION ALL SELECT 'index ' || indexname || ' ' || indexdef FROM pg_indexes WHERE schemaname = 'public' AND tablename <> 'schema_migrations'
		UNION ALL SELECT 'extension ' || extname FROM pg_extension WHERE extname <> 'plpgsql'
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}
