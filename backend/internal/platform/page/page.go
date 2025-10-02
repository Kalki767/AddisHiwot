// Package page implements cursor (keyset) pagination for lists sorted newest first.
//
// Instead of OFFSET, which gets slower the deeper you go and skips or repeats
// rows when new ones arrive, the client sends back an opaque cursor naming the
// last item it saw. The query then asks for rows strictly after that item:
//
//	SELECT ... FROM t
//	WHERE user_id = $1
//	  AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
//	ORDER BY created_at DESC, id DESC
//	LIMIT $4  -- Params.Limit + 1, to learn whether another page exists
//
// The id breaks ties between rows with the same timestamp, so no row is ever
// skipped or shown twice. With an index on (user_id, created_at DESC, id DESC)
// every page costs the same.
//
// The package has no HTTP or database code, so domain code may use its types.
package page

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Page size limits.
const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// ErrBadCursor means a cursor string could not be decoded.
var ErrBadCursor = errors.New("page: invalid cursor")

// Cursor names the last item of a page: its sort time and its id.
type Cursor struct {
	At time.Time
	ID uuid.UUID
}

// Params says which page to load. After is nil for the first page.
type Params struct {
	Limit int
	After *Cursor
}

// FetchLimit is how many rows to ask the database for: one extra row tells
// whether another page follows.
func (p Params) FetchLimit() int { return p.Limit + 1 }

// Page is one page of items. Next is nil on the last page.
type Page[T any] struct {
	Items []T
	Next  *Cursor
}

// Build turns rows fetched with FetchLimit into a page: it drops the extra row
// and, if there was one, sets Next to the cursor of the last item kept.
func Build[T any](rows []T, p Params, key func(T) Cursor) Page[T] {
	if len(rows) <= p.Limit {
		if rows == nil {
			rows = []T{}
		}
		return Page[T]{Items: rows}
	}
	items := rows[:p.Limit]
	next := key(items[len(items)-1])
	return Page[T]{Items: items, Next: &next}
}

type wire struct {
	At string `json:"t"`
	ID string `json:"i"`
}

// Encode turns a cursor into an opaque, URL-safe string.
func Encode(c Cursor) string {
	b, _ := json.Marshal(wire{At: c.At.UTC().Format(time.RFC3339Nano), ID: c.ID.String()})
	return base64.RawURLEncoding.EncodeToString(b)
}

// Decode reverses Encode. Anything else gives ErrBadCursor.
func Decode(s string) (Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, ErrBadCursor
	}
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		return Cursor{}, ErrBadCursor
	}
	at, err := time.Parse(time.RFC3339Nano, w.At)
	if err != nil {
		return Cursor{}, ErrBadCursor
	}
	id, err := uuid.Parse(w.ID)
	if err != nil {
		return Cursor{}, ErrBadCursor
	}
	return Cursor{At: at, ID: id}, nil
}
