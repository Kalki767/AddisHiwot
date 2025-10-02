package httpx

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/page"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/validate"
)

// ParsePage reads ?limit= (1 to page.MaxLimit, default page.DefaultLimit) and
// ?cursor= (from a previous response's next_cursor).
func ParsePage(r *http.Request) (page.Params, error) {
	q := r.URL.Query()
	p := page.Params{Limit: page.DefaultLimit}

	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		var v validate.Validator
		v.Check(err == nil && n >= 1 && n <= page.MaxLimit, "limit", "out_of_range",
			"must be a whole number from 1 to "+strconv.Itoa(page.MaxLimit))
		if err := v.Err(); err != nil {
			return page.Params{}, err
		}
		p.Limit = n
	}
	if s := q.Get("cursor"); s != "" {
		c, err := page.Decode(s)
		if errors.Is(err, page.ErrBadCursor) {
			return page.Params{}, NewError(http.StatusBadRequest, CodeBadCursor,
				"The cursor is not valid. Start again from the first page.")
		}
		p.After = &c
	}
	return p, nil
}

// PageResponse is the JSON shape of every list:
//
//	{"items": [...], "next_cursor": "..."}   next_cursor is null on the last page
type PageResponse[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// NewPageResponse converts a page of domain items with toJSON.
func NewPageResponse[T, J any](p page.Page[T], toJSON func(T) J) PageResponse[J] {
	out := PageResponse[J]{Items: make([]J, 0, len(p.Items))}
	for _, item := range p.Items {
		out.Items = append(out.Items, toJSON(item))
	}
	if p.Next != nil {
		s := page.Encode(*p.Next)
		out.NextCursor = &s
	}
	return out
}
