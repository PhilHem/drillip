package domain

import (
	"errors"
	"testing"
)

func TestListQueryValidate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query ListQuery
		valid bool
	}{
		{name: "default sort", query: ListQuery{Limit: DefaultListLimit}, valid: true},
		{name: "last activity", query: ListQuery{Sort: SortLastSeen, Limit: 1}, valid: true},
		{name: "count and offset", query: ListQuery{Sort: SortCount, Limit: MaxListLimit, Offset: 20}, valid: true},
		{name: "literal search", query: ListQuery{Search: "  %_ ' type:error", Limit: 1}, valid: true},
		{name: "missing limit", query: ListQuery{}},
		{name: "negative limit", query: ListQuery{Limit: -1}},
		{name: "excessive limit", query: ListQuery{Limit: MaxListLimit + 1}},
		{name: "negative offset", query: ListQuery{Limit: 1, Offset: -1}},
		{name: "unsupported sort", query: ListQuery{Limit: 1, Sort: "first_seen"}},
		{name: "sort expression", query: ListQuery{Limit: 1, Sort: "count DESC"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.query.Validate()
			if tc.valid {
				if err != nil {
					t.Fatalf("Validate() = %v", err)
				}
			} else if !errors.Is(err, ErrInvalidListQuery) {
				t.Fatalf("Validate() = %v, want ErrInvalidListQuery", err)
			}
		})
	}
}
