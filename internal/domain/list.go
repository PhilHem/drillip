package domain

import (
	"errors"
	"fmt"
)

const (
	DefaultListLimit = 50
	MaxListLimit     = 500
	SortLastSeen     = "last_seen"
	SortCount        = "count"
)

var ErrInvalidListQuery = errors.New("invalid list query")

// ListQuery selects a page of error groups, including resolved groups.
// Search is a literal, ASCII case-insensitive substring of the stored type or
// message. An empty Sort uses last_seen. Limit must be supplied by the caller.
type ListQuery struct {
	Filter ListFilter
	Search string
	Sort   string
	Limit  int
	Offset int
}

// Validate rejects unsupported ordering and pagination before storage access.
func (q ListQuery) Validate() error {
	if q.Sort != "" && q.Sort != SortLastSeen && q.Sort != SortCount {
		return fmt.Errorf("%w: sort must be %q or %q", ErrInvalidListQuery, SortLastSeen, SortCount)
	}
	if q.Limit < 1 || q.Limit > MaxListLimit {
		return fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidListQuery, MaxListLimit)
	}
	if q.Offset < 0 {
		return fmt.Errorf("%w: offset must not be negative", ErrInvalidListQuery)
	}
	return nil
}

// ErrorPage contains at most the requested limit of error groups. HasMore means
// another matching group existed when the page was read. Separate page reads do
// not share a snapshot, so concurrent ingestion can change page boundaries.
type ErrorPage struct {
	Errors  []ErrorSummary
	HasMore bool
}
