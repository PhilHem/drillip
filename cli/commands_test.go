package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/PhilHem/drillip/store"
)

func setupStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func insertTestError(t *testing.T, s *store.Store, fp, typ, val, release string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.RawDB().Exec(`
		INSERT INTO errors (fingerprint, type, value, level, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform, first_seen, last_seen, count)
		VALUES (?, ?, ?, 'error', ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(fingerprint) DO UPDATE SET
			last_seen = ?, count = count + 1
	`, fp, typ, val,
		`{"frames":[{"filename":"app.py","function":"main","lineno":42}]}`,
		`[{"category":"http","message":"GET /api"}]`,
		release, "production",
		`{"id":"42","email":"test@example.com"}`,
		`{"server":"web-1"}`,
		"python", now, now,
		now)
	if err != nil {
		t.Fatalf("insert error: %v", err)
	}
}

func insertTestOccurrence(t *testing.T, s *store.Store, fp, release, traceID string, ts time.Time) {
	t.Helper()
	_, err := s.RawDB().Exec(`INSERT INTO occurrences (fingerprint, timestamp, release_tag, trace_id) VALUES (?,?,?,?)`,
		fp, ts.Format(time.RFC3339), release, traceID)
	if err != nil {
		t.Fatalf("insert occurrence: %v", err)
	}
}

func insertTestErrorWithTags(t *testing.T, s *store.Store, fp, typ, val, release, tagsJSON string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.RawDB().Exec(`
		INSERT INTO errors (fingerprint, type, value, level, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform, first_seen, last_seen, count)
		VALUES (?, ?, ?, 'error', ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(fingerprint) DO UPDATE SET
			last_seen = ?, count = count + 1
	`, fp, typ, val,
		`{"frames":[{"filename":"app.py","function":"main","lineno":42}]}`,
		`[{"category":"http","message":"GET /api"}]`,
		release, "production",
		`{"id":"42","email":"test@example.com"}`,
		tagsJSON,
		"python", now, now,
		now)
	if err != nil {
		t.Fatalf("insert error: %v", err)
	}
}

func insertTestOccurrenceWithTags(t *testing.T, s *store.Store, fp, release, traceID, tagsJSON string, ts time.Time) {
	t.Helper()
	_, err := s.RawDB().Exec(`INSERT INTO occurrences (fingerprint, timestamp, release_tag, trace_id, tags) VALUES (?,?,?,?,?)`,
		fp, ts.Format(time.RFC3339), release, traceID, tagsJSON)
	if err != nil {
		t.Fatalf("insert occurrence: %v", err)
	}
}

// --- stats ---

func TestRunStats(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	insertTestError(t, s, "aaaa111122223333", "TypeError", "null ref", "v1.0.0")
	insertTestOccurrence(t, s, "aaaa111122223333", "v1.0.0", "", time.Now())

	var buf bytes.Buffer
	c.RunStats(nil, &buf)
	out := buf.String()
	if !strings.Contains(out, "Unique errors:") {
		t.Fatalf("missing unique count: %s", out)
	}
	if !strings.Contains(out, "Total occurrences:") {
		t.Fatalf("missing total: %s", out)
	}
}

func TestRunStatsEmpty(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	var buf bytes.Buffer
	c.RunStats(nil, &buf)
	if !strings.Contains(buf.String(), "0") {
		t.Fatalf("expected 0 for empty db: %s", buf.String())
	}
}

// --- top ---

func TestRunTop(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	insertTestError(t, s, "bbbb111122223333", "ValueError", "bad input", "v1.0.0")
	insertTestError(t, s, "bbbb111122223333", "ValueError", "bad input", "v1.0.0") // increment
	insertTestError(t, s, "cccc111122223333", "IOError", "file not found", "v1.0.0")

	var buf bytes.Buffer
	c.RunTop(nil, &buf)
	out := buf.String()
	if !strings.Contains(out, "ValueError") {
		t.Fatalf("missing ValueError: %s", out)
	}
	if !strings.Contains(out, "IOError") {
		t.Fatalf("missing IOError: %s", out)
	}
	// ValueError should appear first (count=2 > count=1)
	valIdx := strings.Index(out, "ValueError")
	ioIdx := strings.Index(out, "IOError")
	if valIdx > ioIdx {
		t.Fatalf("ValueError should appear before IOError (higher count)")
	}
}

func TestRunTopEmpty(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	var buf bytes.Buffer
	c.RunTop(nil, &buf)
	if !strings.Contains(buf.String(), "no errors") {
		t.Fatalf("expected 'no errors' message: %s", buf.String())
	}
}

func TestRunTopLimit(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	insertTestError(t, s, "dddd111122223333", "Err1", "e1", "v1")
	insertTestError(t, s, "eeee111122223333", "Err2", "e2", "v1")
	insertTestError(t, s, "ffff111122223333", "Err3", "e3", "v1")

	var buf bytes.Buffer
	c.RunTop([]string{"-limit", "2"}, &buf)
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// header + separator + 2 rows + blank + hint = at least 5 lines
	dataLines := 0
	for _, l := range lines {
		if strings.Contains(l, "Err") {
			dataLines++
		}
	}
	if dataLines > 2 {
		t.Fatalf("expected at most 2 data rows, got %d", dataLines)
	}
}

// --- recent ---

func TestRunRecent(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	// Insert with current time — should appear in recent
	insertTestError(t, s, "aaab111122223333", "RecentErr", "just happened", "v2.0.0")

	var buf bytes.Buffer
	c.RunRecent(nil, &buf)
	out := buf.String()
	if !strings.Contains(out, "RecentErr") {
		t.Fatalf("missing recent error: %s", out)
	}
}

func TestRunRecentEmpty(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	var buf bytes.Buffer
	c.RunRecent(nil, &buf)
	if !strings.Contains(buf.String(), "no new errors") {
		t.Fatalf("expected empty message: %s", buf.String())
	}
}

// --- show ---

func TestRunShow(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	insertTestError(t, s, "aaac111122223333", "ShowError", "show me", "v3.0.0")

	var buf bytes.Buffer
	c.RunShow([]string{"aaac"}, &buf) // prefix match
	out := buf.String()
	if !strings.Contains(out, "ShowError") {
		t.Fatalf("missing error type: %s", out)
	}
	if !strings.Contains(out, "show me") {
		t.Fatalf("missing error value: %s", out)
	}
	if !strings.Contains(out, "Stacktrace") {
		t.Fatalf("missing stacktrace section: %s", out)
	}
	if !strings.Contains(out, "Breadcrumbs") {
		t.Fatalf("missing breadcrumbs section: %s", out)
	}
	if !strings.Contains(out, "User") {
		t.Fatalf("missing user section: %s", out)
	}
}

func TestRunShowNotFound(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	var buf bytes.Buffer
	c.RunShow([]string{"0000000000000000"}, &buf)
	if !strings.Contains(buf.String(), "not found") {
		t.Fatalf("expected not found: %s", buf.String())
	}
}

func TestRunShowNoArgs(t *testing.T) {
	c := &CLI{}
	var buf bytes.Buffer
	c.RunShow(nil, &buf)
	if !strings.Contains(buf.String(), "usage") {
		t.Fatalf("expected usage: %s", buf.String())
	}
}

// --- trend ---

func TestRunTrend(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	fp := "aaad111122223333"
	insertTestError(t, s, fp, "TrendErr", "trending", "v1.0.0")

	// Insert occurrences in the last hour
	now := time.Now().UTC()
	insertTestOccurrence(t, s, fp, "v1.0.0", "", now)
	insertTestOccurrence(t, s, fp, "v1.0.0", "", now.Add(-10*time.Minute))
	insertTestOccurrence(t, s, fp, "v1.0.0", "", now.Add(-20*time.Minute))

	var buf bytes.Buffer
	c.RunTrend([]string{"aaad"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "█") {
		t.Fatalf("missing bar chart: %s", out)
	}
}

func TestRunTrendNotFound(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	var buf bytes.Buffer
	c.RunTrend([]string{"0000000000000000"}, &buf)
	if !strings.Contains(buf.String(), "not found") {
		t.Fatalf("expected not found: %s", buf.String())
	}
}

// --- releases ---

func TestRunReleases(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	fp := "aaae111122223333"
	insertTestError(t, s, fp, "RelErr", "release test", "v1.0.0")

	now := time.Now().UTC()
	insertTestOccurrence(t, s, fp, "v1.0.0", "", now)
	insertTestOccurrence(t, s, fp, "v1.0.0", "", now.Add(-time.Hour))
	insertTestOccurrence(t, s, fp, "v2.0.0", "", now)

	var buf bytes.Buffer
	c.RunReleases([]string{"aaae"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "v1.0.0") || !strings.Contains(out, "v2.0.0") {
		t.Fatalf("missing releases: %s", out)
	}
}

// --- gc ---

func TestRunGC(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	fp := "aaaf111122223333"
	insertTestError(t, s, fp, "GCErr", "old error", "v1.0.0")

	// Insert an old occurrence
	old := time.Now().UTC().Add(-48 * time.Hour)
	insertTestOccurrence(t, s, fp, "v1.0.0", "", old)

	// Insert a recent occurrence
	insertTestOccurrence(t, s, fp, "v1.0.0", "", time.Now().UTC())

	var buf bytes.Buffer
	c.RunGC([]string{"24h"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "deleted 1") {
		t.Fatalf("expected 1 deleted: %s", out)
	}

	// Verify only 1 remaining
	var count int
	if err := s.RawDB().QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 remaining occurrence, got %d", count)
	}
}

func TestRunGCNoArgs(t *testing.T) {
	c := &CLI{}
	var buf bytes.Buffer
	c.RunGC(nil, &buf)
	if !strings.Contains(buf.String(), "usage") {
		t.Fatalf("expected usage: %s", buf.String())
	}
}

// --- correlate ---

func TestRunCorrelateNoArgs(t *testing.T) {
	c := &CLI{}
	var buf bytes.Buffer
	c.RunCorrelate(nil, &buf)
	if !strings.Contains(buf.String(), "usage") {
		t.Fatalf("expected usage: %s", buf.String())
	}
}

func TestRunCorrelateNoIntegrations(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	fp := "cccc222233334444"
	insertTestError(t, s, fp, "CorrelateErr", "correlate test", "v1.0.0")
	insertTestOccurrence(t, s, fp, "v1.0.0", "", time.Now().UTC())

	var buf bytes.Buffer
	c.RunCorrelate([]string{fp[:4]}, &buf)
	out := buf.String()
	if !strings.Contains(out, "CorrelateErr") {
		t.Fatalf("missing error type: %s", out)
	}
	if !strings.Contains(out, "Stacktrace") {
		t.Fatalf("missing stacktrace section: %s", out)
	}
	if !strings.Contains(out, "Breadcrumbs") {
		t.Fatalf("missing breadcrumbs section: %s", out)
	}
}

func TestRunCorrelateNotFound(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	var buf bytes.Buffer
	c.RunCorrelate([]string{"0000000000000000"}, &buf)
	if !strings.Contains(buf.String(), "not found") {
		t.Fatalf("expected not found: %s", buf.String())
	}
}

// --- tag filtering ---

func TestRunTopWithTagFilter(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	insertTestErrorWithTags(t, s, "aab0111122223333", "TagErr1", "on web-1", "v1.0.0", `{"server":"web-1"}`)
	insertTestErrorWithTags(t, s, "aab1222233334444", "TagErr2", "on web-2", "v1.0.0", `{"server":"web-2"}`)

	var buf bytes.Buffer
	c.RunTop([]string{"--tag", "server=web-1"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "TagErr1") {
		t.Fatalf("missing TagErr1: %s", out)
	}
	if strings.Contains(out, "TagErr2") {
		t.Fatalf("should not contain TagErr2: %s", out)
	}
}

func TestRunRecentWithTagFilter(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	insertTestErrorWithTags(t, s, "aab2111122223333", "RecentTag1", "tagged recent", "v1.0.0", `{"endpoint":"/api/orders"}`)
	insertTestErrorWithTags(t, s, "aab3222233334444", "RecentTag2", "other recent", "v1.0.0", `{"endpoint":"/api/users"}`)

	var buf bytes.Buffer
	c.RunRecent([]string{"--tag", "endpoint=/api/orders"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "RecentTag1") {
		t.Fatalf("missing RecentTag1: %s", out)
	}
	if strings.Contains(out, "RecentTag2") {
		t.Fatalf("should not contain RecentTag2: %s", out)
	}
}

// --- resolve ---

func TestRunResolve(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	fp := "aab4111122223333"
	insertTestError(t, s, fp, "ResolveErr", "needs resolving", "v1.0.0")

	var buf bytes.Buffer
	c.RunResolve([]string{"aab4"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "resolved 1") {
		t.Fatalf("expected resolve confirmation: %s", out)
	}

	// Verify resolved_at is set
	var resolvedAt string
	if err := s.RawDB().QueryRow("SELECT COALESCE(resolved_at, '') FROM errors WHERE fingerprint = ?", fp).Scan(&resolvedAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if resolvedAt == "" {
		t.Fatal("expected resolved_at to be set")
	}
}

func TestRunResolveNotFound(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}

	var buf bytes.Buffer
	c.RunResolve([]string{"0000000000000000"}, &buf)
	if !strings.Contains(buf.String(), "no unresolved") {
		t.Fatalf("expected not found message: %s", buf.String())
	}
}

func TestRunResolveNoArgs(t *testing.T) {
	c := &CLI{}
	var buf bytes.Buffer
	c.RunResolve(nil, &buf)
	if !strings.Contains(buf.String(), "usage") {
		t.Fatalf("expected usage: %s", buf.String())
	}
}

// --- state column in top/recent ---

func TestRunTopShowsState(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	insertTestError(t, s, "aab5111122223333", "StateErr", "state test", "v1.0.0")

	var buf bytes.Buffer
	c.RunTop(nil, &buf)
	out := buf.String()
	if !strings.Contains(out, "STATE") {
		t.Fatalf("missing STATE column header: %s", out)
	}
}

// --- silence ---

func TestRunSilence(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}

	var buf bytes.Buffer
	c.RunSilence([]string{"abc123"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "silenced abc123 permanently") {
		t.Fatalf("expected permanent silence message: %s", out)
	}

	if !s.IsSilenced("abc123") {
		t.Fatal("expected fingerprint to be silenced")
	}
}

func TestRunSilenceWithDuration(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}

	var buf bytes.Buffer
	c.RunSilence([]string{"d0a123", "2h"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "silenced d0a123 until") {
		t.Fatalf("expected timed silence message: %s", out)
	}

	if !s.IsSilenced("d0a123") {
		t.Fatal("expected fingerprint to be silenced")
	}
}

func TestRunSilenceWithReason(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}

	var buf bytes.Buffer
	c.RunSilence([]string{"--reason", "maintenance", "a5b123"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "silenced a5b123 permanently") {
		t.Fatalf("expected permanent silence message: %s", out)
	}

	entries, _ := s.ListSilences()
	found := false
	for _, e := range entries {
		if e.Fingerprint == "a5b123" && e.Reason == "maintenance" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected silence with reason 'maintenance'")
	}
}

func TestRunSilenceNoArgs(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}

	var buf bytes.Buffer
	c.RunSilence(nil, &buf)
	if !strings.Contains(buf.String(), "usage") {
		t.Fatalf("expected usage: %s", buf.String())
	}
}

func TestRunSilences(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}

	_ = s.Silence("a0e111", nil, "test reason")

	var buf bytes.Buffer
	c.RunSilences(nil, &buf)
	out := buf.String()
	if !strings.Contains(out, "a0e111") {
		t.Fatalf("missing fingerprint in silences: %s", out)
	}
	if !strings.Contains(out, "test reason") {
		t.Fatalf("missing reason in silences: %s", out)
	}
}

func TestRunSilencesEmpty(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}

	var buf bytes.Buffer
	c.RunSilences(nil, &buf)
	if !strings.Contains(buf.String(), "no active silences") {
		t.Fatalf("expected empty message: %s", buf.String())
	}
}

func TestRunUnsilence(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}

	_ = s.Silence("a0b0c123", nil, "")

	var buf bytes.Buffer
	c.RunUnsilence([]string{"a0b0c123"}, &buf)
	if !strings.Contains(buf.String(), "unsilenced a0b0c123") {
		t.Fatalf("expected unsilence confirmation: %s", buf.String())
	}

	if s.IsSilenced("a0b0c123") {
		t.Fatal("expected fingerprint to no longer be silenced")
	}
}

func TestRunUnsilenceNoArgs(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}

	var buf bytes.Buffer
	c.RunUnsilence(nil, &buf)
	if !strings.Contains(buf.String(), "usage") {
		t.Fatalf("expected usage: %s", buf.String())
	}
}

func TestShowTagDistribution(t *testing.T) {
	s := setupStore(t)
	c := &CLI{Store: s}
	fp := "aab6111122223333"
	insertTestErrorWithTags(t, s, fp, "DistErr", "distribution test", "v1.0.0", `{"server":"web-1"}`)

	now := time.Now().UTC()
	insertTestOccurrenceWithTags(t, s, fp, "v1.0.0", "", `{"server":"web-1"}`, now)
	insertTestOccurrenceWithTags(t, s, fp, "v1.0.0", "", `{"server":"web-1"}`, now.Add(-time.Minute))
	insertTestOccurrenceWithTags(t, s, fp, "v1.0.0", "", `{"server":"web-2"}`, now.Add(-2*time.Minute))

	var buf bytes.Buffer
	c.RunShow([]string{"aab6"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "Tag Distribution") {
		t.Fatalf("missing tag distribution section: %s", out)
	}
	if !strings.Contains(out, "web-1") {
		t.Fatalf("missing web-1 in distribution: %s", out)
	}
	if !strings.Contains(out, "web-2") {
		t.Fatalf("missing web-2 in distribution: %s", out)
	}
	if !strings.Contains(out, "66%") {
		t.Fatalf("missing percentage for web-1: %s", out)
	}
}
