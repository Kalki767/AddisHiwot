package page_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/page"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/testdb"
)

func TestCursorRoundTrip(t *testing.T) {
	c := page.Cursor{At: time.Date(2026, 10, 26, 8, 30, 0, 123456000, time.UTC), ID: uuid.New()}
	got, err := page.Decode(page.Encode(c))
	if err != nil || !got.At.Equal(c.At) || got.ID != c.ID {
		t.Errorf("round trip: got %+v, %v; want %+v", got, err, c)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "!!!", "bm90IGpzb24", // "not json"
		page.Encode(page.Cursor{})[:5],
		"eyJ0IjoieWVzdGVyZGF5IiwiaSI6IjEifQ", // {"t":"yesterday","i":"1"}
	} {
		if _, err := page.Decode(s); !errors.Is(err, page.ErrBadCursor) {
			t.Errorf("Decode(%q) err = %v, want ErrBadCursor", s, err)
		}
	}
}

func TestBuild(t *testing.T) {
	key := func(n int) page.Cursor { return page.Cursor{ID: uuid.UUID{byte(n)}} }
	p := page.Params{Limit: 3}

	full := page.Build([]int{1, 2, 3, 4}, p, key) // 4 rows = limit + 1: another page exists
	if len(full.Items) != 3 || full.Next == nil || full.Next.ID != key(3).ID {
		t.Errorf("with an extra row: %+v", full)
	}
	last := page.Build([]int{1, 2}, p, key)
	if len(last.Items) != 2 || last.Next != nil {
		t.Errorf("last page: %+v", last)
	}
	empty := page.Build[int](nil, p, key)
	if empty.Items == nil || len(empty.Items) != 0 || empty.Next != nil {
		t.Errorf("empty page must have an empty (not nil) list: %+v", empty)
	}
}

// Paging through real rows with the documented query must return every row
// exactly once, newest first, even when many rows share a timestamp.
func TestKeysetPagingAgainstPostgres(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE items (id uuid PRIMARY KEY DEFAULT uuidv7(), created_at timestamptz NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	// 25 rows over 5 timestamps: 5 rows share each timestamp.
	if _, err := pool.Exec(ctx, `INSERT INTO items (created_at)
		SELECT timestamptz '2026-10-26 08:00:00+03' + (n / 5) * interval '1 minute' FROM generate_series(0, 24) AS n`); err != nil {
		t.Fatal(err)
	}
	type row struct {
		ID uuid.UUID
		At time.Time
	}
	rows, err := pool.Query(ctx, `SELECT id FROM items ORDER BY created_at DESC, id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	want, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		t.Fatal(err)
	}

	var got []uuid.UUID
	var cursor string
	pages := 0
	for {
		p := page.Params{Limit: 10}
		if cursor != "" {
			c, err := page.Decode(cursor)
			if err != nil {
				t.Fatal(err)
			}
			p.After = &c
		}
		var at *time.Time
		var id *uuid.UUID
		if p.After != nil {
			at, id = &p.After.At, &p.After.ID
		}
		rows, err := pool.Query(ctx, `SELECT id, created_at FROM items
			WHERE ($1::timestamptz IS NULL OR (created_at, id) < ($1, $2))
			ORDER BY created_at DESC, id DESC LIMIT $3`, at, id, p.FetchLimit())
		if err != nil {
			t.Fatal(err)
		}
		fetched, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
		if err != nil {
			t.Fatal(err)
		}
		pg := page.Build(fetched, p, func(r row) page.Cursor { return page.Cursor{At: r.At, ID: r.ID} })
		pages++
		for _, r := range pg.Items {
			got = append(got, r.ID)
		}
		if pg.Next == nil {
			break
		}
		cursor = page.Encode(*pg.Next)
	}

	if pages != 3 {
		t.Errorf("pages = %d, want 3 (10 + 10 + 5)", pages)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d differs: paging skipped or repeated a row", i)
		}
	}
}
