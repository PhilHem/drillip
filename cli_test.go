package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func insertTestError(t *testing.T, fp, typ, val, release string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO errors (fingerprint, type, value, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform, first_seen, last_seen, count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
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

func insertTestOccurrence(t *testing.T, fp, release, traceID string, ts time.Time) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO occurrences (fingerprint, timestamp, release_tag, trace_id) VALUES (?,?,?,?)`,
		fp, ts.Format(time.RFC3339), release, traceID)
	if err != nil {
		t.Fatalf("insert occurrence: %v", err)
	}
}

// --- stats ---

func TestRunStats(t *testing.T) {
	setupTestDB(t)
	insertTestError(t, "aaaa111122223333", "TypeError", "null ref", "v1.0.0")
	insertTestOccurrence(t, "aaaa111122223333", "v1.0.0", "", time.Now())

	var buf bytes.Buffer
	runStats(nil, &buf)
	out := buf.String()
	if !strings.Contains(out, "Unique errors:") {
		t.Fatalf("missing unique count: %s", out)
	}
	if !strings.Contains(out, "Total occurrences:") {
		t.Fatalf("missing total: %s", out)
	}
}

func TestRunStatsEmpty(t *testing.T) {
	setupTestDB(t)
	var buf bytes.Buffer
	runStats(nil, &buf)
	if !strings.Contains(buf.String(), "0") {
		t.Fatalf("expected 0 for empty db: %s", buf.String())
	}
}

// --- top ---

func TestRunTop(t *testing.T) {
	setupTestDB(t)
	insertTestError(t, "bbbb111122223333", "ValueError", "bad input", "v1.0.0")
	insertTestError(t, "bbbb111122223333", "ValueError", "bad input", "v1.0.0") // increment
	insertTestError(t, "cccc111122223333", "IOError", "file not found", "v1.0.0")

	var buf bytes.Buffer
	runTop(nil, &buf)
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
	setupTestDB(t)
	var buf bytes.Buffer
	runTop(nil, &buf)
	if !strings.Contains(buf.String(), "no errors") {
		t.Fatalf("expected 'no errors' message: %s", buf.String())
	}
}

func TestRunTopLimit(t *testing.T) {
	setupTestDB(t)
	insertTestError(t, "dddd111122223333", "Err1", "e1", "v1")
	insertTestError(t, "eeee111122223333", "Err2", "e2", "v1")
	insertTestError(t, "ffff111122223333", "Err3", "e3", "v1")

	var buf bytes.Buffer
	runTop([]string{"-limit", "2"}, &buf)
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
	setupTestDB(t)
	// Insert with current time — should appear in recent
	insertTestError(t, "rrrr111122223333", "RecentErr", "just happened", "v2.0.0")

	var buf bytes.Buffer
	runRecent(nil, &buf)
	out := buf.String()
	if !strings.Contains(out, "RecentErr") {
		t.Fatalf("missing recent error: %s", out)
	}
}

func TestRunRecentEmpty(t *testing.T) {
	setupTestDB(t)
	var buf bytes.Buffer
	runRecent(nil, &buf)
	if !strings.Contains(buf.String(), "no new errors") {
		t.Fatalf("expected empty message: %s", buf.String())
	}
}

// --- show ---

func TestRunShow(t *testing.T) {
	setupTestDB(t)
	insertTestError(t, "ssss111122223333", "ShowError", "show me", "v3.0.0")

	var buf bytes.Buffer
	runShow([]string{"ssss"}, &buf) // prefix match
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
	setupTestDB(t)
	var buf bytes.Buffer
	runShow([]string{"nonexistent"}, &buf)
	if !strings.Contains(buf.String(), "not found") {
		t.Fatalf("expected not found: %s", buf.String())
	}
}

func TestRunShowNoArgs(t *testing.T) {
	var buf bytes.Buffer
	runShow(nil, &buf)
	if !strings.Contains(buf.String(), "usage") {
		t.Fatalf("expected usage: %s", buf.String())
	}
}

// --- trend ---

func TestRunTrend(t *testing.T) {
	setupTestDB(t)
	fp := "tttt111122223333"
	insertTestError(t, fp, "TrendErr", "trending", "v1.0.0")

	// Insert occurrences in the last hour
	now := time.Now().UTC()
	insertTestOccurrence(t, fp, "v1.0.0", "", now)
	insertTestOccurrence(t, fp, "v1.0.0", "", now.Add(-10*time.Minute))
	insertTestOccurrence(t, fp, "v1.0.0", "", now.Add(-20*time.Minute))

	var buf bytes.Buffer
	runTrend([]string{"tttt"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "█") {
		t.Fatalf("missing bar chart: %s", out)
	}
}

func TestRunTrendNotFound(t *testing.T) {
	setupTestDB(t)
	var buf bytes.Buffer
	runTrend([]string{"nonexistent"}, &buf)
	if !strings.Contains(buf.String(), "not found") {
		t.Fatalf("expected not found: %s", buf.String())
	}
}

// --- releases ---

func TestRunReleases(t *testing.T) {
	setupTestDB(t)
	fp := "llll111122223333"
	insertTestError(t, fp, "RelErr", "release test", "v1.0.0")

	now := time.Now().UTC()
	insertTestOccurrence(t, fp, "v1.0.0", "", now)
	insertTestOccurrence(t, fp, "v1.0.0", "", now.Add(-time.Hour))
	insertTestOccurrence(t, fp, "v2.0.0", "", now)

	var buf bytes.Buffer
	runReleases([]string{"llll"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "v1.0.0") || !strings.Contains(out, "v2.0.0") {
		t.Fatalf("missing releases: %s", out)
	}
}

// --- gc ---

func TestRunGC(t *testing.T) {
	setupTestDB(t)
	fp := "gggg111122223333"
	insertTestError(t, fp, "GCErr", "old error", "v1.0.0")

	// Insert an old occurrence
	old := time.Now().UTC().Add(-48 * time.Hour)
	insertTestOccurrence(t, fp, "v1.0.0", "", old)

	// Insert a recent occurrence
	insertTestOccurrence(t, fp, "v1.0.0", "", time.Now().UTC())

	var buf bytes.Buffer
	runGC([]string{"24h"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "deleted 1") {
		t.Fatalf("expected 1 deleted: %s", out)
	}

	// Verify only 1 remaining
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 remaining occurrence, got %d", count)
	}
}

func TestRunGCNoArgs(t *testing.T) {
	var buf bytes.Buffer
	runGC(nil, &buf)
	if !strings.Contains(buf.String(), "usage") {
		t.Fatalf("expected usage: %s", buf.String())
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
		err   bool
	}{
		{"24h", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"", 0, true},
		{"abc", 0, true},
		{"10x", 0, true},
	}

	for _, tt := range tests {
		got, err := parseDuration(tt.input)
		if tt.err && err == nil {
			t.Errorf("parseDuration(%q): expected error", tt.input)
		}
		if !tt.err && err != nil {
			t.Errorf("parseDuration(%q): unexpected error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// --- correlate ---

func TestRunCorrelateNoArgs(t *testing.T) {
	var buf bytes.Buffer
	runCorrelate(nil, &buf, Config{})
	if !strings.Contains(buf.String(), "usage") {
		t.Fatalf("expected usage: %s", buf.String())
	}
}

func TestRunCorrelateNoIntegrations(t *testing.T) {
	setupTestDB(t)
	fp := "cccc222233334444"
	insertTestError(t, fp, "CorrelateErr", "correlate test", "v1.0.0")
	insertTestOccurrence(t, fp, "v1.0.0", "", time.Now().UTC())

	var buf bytes.Buffer
	runCorrelate([]string{fp[:4]}, &buf, Config{})
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
	setupTestDB(t)
	var buf bytes.Buffer
	runCorrelate([]string{"nonexistent"}, &buf, Config{})
	if !strings.Contains(buf.String(), "not found") {
		t.Fatalf("expected not found: %s", buf.String())
	}
}
