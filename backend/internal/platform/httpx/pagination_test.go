package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/page"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/validate"
)

func parse(query string) (page.Params, error) {
	return ParsePage(httptest.NewRequest(http.MethodGet, "/v1/goals?"+query, nil))
}

func TestParsePageDefaults(t *testing.T) {
	p, err := parse("")
	if err != nil || p.Limit != page.DefaultLimit || p.After != nil {
		t.Errorf("got %+v, %v", p, err)
	}
}

func TestParsePageReadsLimitAndCursor(t *testing.T) {
	c := page.Cursor{At: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC), ID: uuid.New()}
	p, err := parse("limit=5&cursor=" + page.Encode(c))
	if err != nil || p.Limit != 5 || p.After == nil || p.After.ID != c.ID {
		t.Errorf("got %+v, %v", p, err)
	}
}

func TestParsePageRejectsBadLimits(t *testing.T) {
	for _, limit := range []string{"0", "-1", "101", "ten", "1.5"} {
		_, err := parse("limit=" + limit)
		var fields validate.Errors
		if !errors.As(err, &fields) || fields[0].Field != "limit" {
			t.Errorf("limit=%s: err = %v, want a validation error on limit", limit, err)
		}
	}
}

func TestParsePageRejectsBadCursor(t *testing.T) {
	_, err := parse("cursor=not-a-cursor")
	var he *Error
	if !errors.As(err, &he) || he.Code != CodeBadCursor || he.Status != http.StatusBadRequest {
		t.Errorf("err = %v, want 400 BAD_CURSOR", err)
	}
}

func TestPageResponseJSON(t *testing.T) {
	next := page.Cursor{At: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC), ID: uuid.New()}
	withNext := NewPageResponse(page.Page[int]{Items: []int{1, 2}, Next: &next}, func(n int) int { return n * 10 })
	b, _ := json.Marshal(withNext)
	want := `{"items":[10,20],"next_cursor":"` + page.Encode(next) + `"}`
	if string(b) != want {
		t.Errorf("got %s, want %s", b, want)
	}

	last := NewPageResponse(page.Page[int]{Items: []int{}}, func(n int) int { return n })
	b, _ = json.Marshal(last)
	if string(b) != `{"items":[],"next_cursor":null}` {
		t.Errorf("last page = %s, want empty items and null next_cursor", b)
	}
}
