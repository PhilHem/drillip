package sqlite

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

func insertListError(t *testing.T, s *Store, e domain.ErrorSummary, tags string) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO errors
		(fingerprint, count, type, value, level, first_seen, last_seen, resolved_at, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Fingerprint, e.Count, e.Type, e.Value, e.Level, e.FirstSeen, e.LastSeen, e.ResolvedAt, tags)
	if err != nil {
		t.Fatal(err)
	}
}

func listFingerprints(entries []domain.ErrorSummary) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Fingerprint)
	}
	return result
}

func TestListFindsOldRareErrorWhenItReappears(t *testing.T) {
	s := setupStore(t)
	event := domain.Event{Message: "rare recurring failure"}
	first, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatal(err)
	}
	old := "2020-01-01T00:00:00Z"
	if _, err := s.db.Exec(`UPDATE errors SET first_seen = ?, last_seen = ? WHERE fingerprint = ?`, old, old, first.Fingerprint); err != nil {
		t.Fatal(err)
	}
	for i := range domain.DefaultListLimit + 1 {
		insertListError(t, s, domain.ErrorSummary{
			Fingerprint: fmt.Sprintf("%016x", i), Count: 100 + i,
			Type: "CommonError", Value: "frequent failure", Level: "error",
			FirstSeen: old, LastSeen: "2021-01-01T00:00:00Z",
		}, "{}")
	}
	if _, err := s.StoreEvent(&event); err != nil {
		t.Fatal(err)
	}

	page, err := s.List(domain.ListQuery{Limit: domain.DefaultListLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Errors) != domain.DefaultListLimit || !page.HasMore {
		t.Fatalf("page size=%d has_more=%v", len(page.Errors), page.HasMore)
	}
	if got := page.Errors[0]; got.Fingerprint != first.Fingerprint || got.Count != 2 || got.FirstSeen != old {
		t.Fatalf("most recently seen error = %+v", got)
	}
	top, err := s.ListTop(domain.ListFilter{}, domain.DefaultListLimit)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range top {
		if entry.Fingerprint == first.Fingerprint {
			t.Fatal("rare fixture unexpectedly appears in top ranking")
		}
	}
	recent, err := s.ListRecent(domain.ListFilter{}, time.Now().Add(-24*time.Hour))
	if err != nil || len(recent) != 0 {
		t.Fatalf("first-seen view = %+v, err=%v", recent, err)
	}
}

func TestListSortsByActivityOrCountAndIncludesResolved(t *testing.T) {
	s := setupStore(t)
	for _, entry := range []domain.ErrorSummary{
		{Fingerprint: "a", Count: 2, LastSeen: "2026-09-30T00:00:00Z"},
		{Fingerprint: "b", Count: 100, LastSeen: "2026-09-28T00:00:00Z"},
		{Fingerprint: "c", Count: 100, LastSeen: "2026-09-29T00:00:00Z"},
		{Fingerprint: "d", Count: 100, LastSeen: "2026-09-29T00:00:00Z", ResolvedAt: "2026-09-30T00:00:00Z"},
	} {
		insertListError(t, s, entry, "{}")
	}
	for _, tc := range []struct {
		sort string
		want []string
	}{
		{"", []string{"a", "c", "d", "b"}},
		{domain.SortLastSeen, []string{"a", "c", "d", "b"}},
		{domain.SortCount, []string{"c", "d", "b", "a"}},
	} {
		page, err := s.List(domain.ListQuery{Limit: 10, Sort: tc.sort})
		if err != nil || page.HasMore || !reflect.DeepEqual(listFingerprints(page.Errors), tc.want) {
			t.Fatalf("sort=%q page=%+v err=%v", tc.sort, page, err)
		}
		for _, entry := range page.Errors {
			if entry.Fingerprint == "d" && (entry.State != "resolved" || entry.ResolvedAt == "") {
				t.Fatalf("resolved summary = %+v", entry)
			}
		}
	}
}

func TestListSearchUsesFullStoredTextAndLiteralSubstrings(t *testing.T) {
	s := setupStore(t)
	longValue := strings.Repeat("prefix ", 80) + "NeedleAtEnd"
	for _, fixture := range []struct {
		entry domain.ErrorSummary
		tags  string
	}{
		{domain.ErrorSummary{Fingerprint: "a", Type: "DatabaseTimeout", Value: longValue, Level: "error"}, `{"server":"web-1"}`},
		{domain.ErrorSummary{Fingerprint: "b", Type: "PaymentError", Value: "literal 100%_ and 'quoted'", Level: "warning"}, `{"server":"api-1"}`},
		{domain.ErrorSummary{Fingerprint: "c", Type: "OtherError", Value: "literal 100AB and quoted", Level: "error"}, `{"server":"web-2"}`},
	} {
		insertListError(t, s, fixture.entry, fixture.tags)
	}
	for _, tc := range []struct {
		name   string
		search string
		filter domain.ListFilter
		want   []string
	}{
		{name: "type insensitive", search: "databaseTIMEOUT", want: []string{"a"}},
		{name: "complete message", search: "needleATend", want: []string{"a"}},
		{name: "literal wildcards", search: "%_", want: []string{"b"}},
		{name: "literal percent", search: "%", want: []string{"b"}},
		{name: "literal underscore", search: "_", want: []string{"b"}},
		{name: "literal quote", search: "'quoted'", want: []string{"b"}},
		{name: "bound search", search: "' OR 1=1 --", want: []string{}},
		{name: "no search syntax", search: "type:DatabaseTimeout", want: []string{}},
		{name: "level and tag", search: "literal", filter: domain.ListFilter{Level: "error", TagKey: "server", TagVal: "web-2"}, want: []string{"c"}},
		{name: "mismatched tag", search: "literal", filter: domain.ListFilter{Level: "warning", TagKey: "server", TagVal: "web-2"}, want: []string{}},
		{name: "type respects filter", search: "DatabaseTimeout", filter: domain.ListFilter{Level: "warning"}, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := s.List(domain.ListQuery{Search: tc.search, Filter: tc.filter, Limit: 10})
			if err != nil || page.HasMore || !reflect.DeepEqual(listFingerprints(page.Errors), tc.want) {
				t.Fatalf("page=%+v err=%v, want fingerprints=%v", page, err, tc.want)
			}
			if len(page.Errors) == 1 && page.Errors[0].Fingerprint == "a" && page.Errors[0].Value != longValue {
				t.Fatal("stored message was truncated")
			}
		})
	}
}

func TestListFilterUsesStoredGroupTags(t *testing.T) {
	s := setupStore(t)
	event := domain.Event{Message: "same failure", Tags: map[string]string{"server": "original"}}
	result, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatal(err)
	}
	event.Tags["server"] = "latest"
	if _, err := s.StoreEvent(&event); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"original", "latest"} {
		page, err := s.List(domain.ListQuery{Filter: domain.ListFilter{TagKey: "server", TagVal: tag}, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if tag == "original" {
			if len(page.Errors) != 1 || page.Errors[0].Fingerprint != result.Fingerprint {
				t.Fatalf("stored group tag result = %+v", page)
			}
		} else if len(page.Errors) != 0 {
			t.Fatalf("occurrence tag unexpectedly matched group = %+v", page)
		}
	}
}

func TestListPaginationBreaksTiesAndReportsExhaustion(t *testing.T) {
	s := setupStore(t)
	for _, fingerprint := range []string{"e", "c", "a", "d", "b"} {
		insertListError(t, s, domain.ErrorSummary{Fingerprint: fingerprint, Count: 3, LastSeen: "2026-09-30T00:00:00Z"}, "{}")
	}
	for _, sort := range []string{domain.SortLastSeen, domain.SortCount} {
		for _, tc := range []struct {
			offset  int
			limit   int
			want    []string
			hasMore bool
		}{
			{0, 2, []string{"a", "b"}, true},
			{2, 2, []string{"c", "d"}, true},
			{4, 2, []string{"e"}, false},
			{3, 2, []string{"d", "e"}, false},
			{5, 2, []string{}, false},
			{20, 2, []string{}, false},
			{0, domain.MaxListLimit, []string{"a", "b", "c", "d", "e"}, false},
		} {
			page, err := s.List(domain.ListQuery{Sort: sort, Limit: tc.limit, Offset: tc.offset})
			if err != nil || page.HasMore != tc.hasMore || !reflect.DeepEqual(listFingerprints(page.Errors), tc.want) {
				t.Fatalf("sort=%q offset=%d page=%+v err=%v", sort, tc.offset, page, err)
			}
		}
	}
}

func TestListValidatesBeforeQueryingStorage(t *testing.T) {
	// A nil database makes accidental storage access fail immediately.
	s := &Store{}
	for _, query := range []domain.ListQuery{{}, {Limit: 1, Sort: "invalid"}, {Limit: 1, Offset: -1}} {
		if _, err := s.List(query); !errors.Is(err, domain.ErrInvalidListQuery) {
			t.Fatalf("query=%+v err=%v", query, err)
		}
	}
}
