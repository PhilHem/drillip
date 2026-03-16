package store

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/PhilHem/drillip/domain"
)

func setupStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOccurrenceInsertion(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		EventID: "occ-test",
		Release: "v3.0.0",
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "TestError",
				Value: "occ test",
				Stacktrace: &domain.Stacktrace{
					Frames: []domain.Frame{{Filename: "occ.go", Function: "doStuff", Lineno: 1}},
				},
			}},
		},
	}

	if _, err := s.StoreEvent(&event); err != nil {
		t.Fatalf("store: %v", err)
	}

	var occCount int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&occCount); err != nil {
		t.Fatalf("query: %v", err)
	}
	if occCount != 1 {
		t.Fatalf("expected 1 occurrence, got %d", occCount)
	}

	// Send again — should get 2 occurrences
	if _, err := s.StoreEvent(&event); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&occCount); err != nil {
		t.Fatalf("query: %v", err)
	}
	if occCount != 2 {
		t.Fatalf("expected 2 occurrences, got %d", occCount)
	}

	// Verify release_tag
	var release string
	if err := s.DB.QueryRow("SELECT release_tag FROM occurrences LIMIT 1").Scan(&release); err != nil {
		t.Fatalf("query: %v", err)
	}
	if release != "v3.0.0" {
		t.Fatalf("expected release v3.0.0, got %q", release)
	}
}

func TestStoreEventReportsIsNew(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type: "NewTestErr", Value: "first",
				Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "n.go", Function: "f", Lineno: 1}}},
			}},
		},
	}

	result, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if !result.IsNew {
		t.Fatal("first occurrence should be new")
	}

	result, err = s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if result.IsNew {
		t.Fatal("second occurrence should not be new")
	}
}

func TestOccurrenceTraceID(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		EventID: "trace-test",
		Release: "v1.0.0",
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "TraceError",
				Value: "trace test",
				Stacktrace: &domain.Stacktrace{
					Frames: []domain.Frame{{Filename: "t.go", Function: "fn", Lineno: 1}},
				},
			}},
		},
		Contexts: map[string]json.RawMessage{
			"trace": json.RawMessage(`{"trace_id":"deadbeef12345678"}`),
		},
	}

	if _, err := s.StoreEvent(&event); err != nil {
		t.Fatalf("store: %v", err)
	}

	var traceID string
	if err := s.DB.QueryRow("SELECT trace_id FROM occurrences LIMIT 1").Scan(&traceID); err != nil {
		t.Fatalf("query: %v", err)
	}
	if traceID != "deadbeef12345678" {
		t.Fatalf("expected deadbeef12345678, got %q", traceID)
	}
}

func TestAutoResolve(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type: "OldError", Value: "stale",
				Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "old.go", Function: "f", Lineno: 1}}},
			}},
		},
	}

	result, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Backdate last_seen to 48 hours ago
	oldTime := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	if _, err := s.DB.Exec("UPDATE errors SET last_seen = ? WHERE fingerprint = ?", oldTime, result.Fingerprint); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// Auto-resolve with 24h threshold
	n, err := s.AutoResolve(24 * time.Hour)
	if err != nil {
		t.Fatalf("auto-resolve: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 resolved, got %d", n)
	}

	// Verify resolved_at is set
	var resolvedAt sql.NullString
	if err := s.DB.QueryRow("SELECT resolved_at FROM errors WHERE fingerprint = ?", result.Fingerprint).Scan(&resolvedAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !resolvedAt.Valid || resolvedAt.String == "" {
		t.Fatal("expected resolved_at to be set")
	}
}

func TestManualResolve(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type: "ManualResolveErr", Value: "fix me",
				Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "m.go", Function: "f", Lineno: 1}}},
			}},
		},
	}

	result, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	n, err := s.Resolve(result.Fingerprint[:8])
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 resolved, got %d", n)
	}

	// Verify resolved_at is set
	var resolvedAt sql.NullString
	if err := s.DB.QueryRow("SELECT resolved_at FROM errors WHERE fingerprint = ?", result.Fingerprint).Scan(&resolvedAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !resolvedAt.Valid || resolvedAt.String == "" {
		t.Fatal("expected resolved_at to be set")
	}

	// Resolving again should affect 0 rows
	n, err = s.Resolve(result.Fingerprint[:8])
	if err != nil {
		t.Fatalf("resolve again: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 on second resolve, got %d", n)
	}
}

func TestRegressionDetection(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type: "RegressionErr", Value: "comes back",
				Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "r.go", Function: "f", Lineno: 1}}},
			}},
		},
	}

	// First store — new
	result, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if !result.IsNew {
		t.Fatal("first occurrence should be new")
	}
	if result.IsRegression {
		t.Fatal("first occurrence should not be a regression")
	}

	// Resolve it
	_, err = s.Resolve(result.Fingerprint)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Verify it's resolved
	var resolvedAt sql.NullString
	if err := s.DB.QueryRow("SELECT resolved_at FROM errors WHERE fingerprint = ?", result.Fingerprint).Scan(&resolvedAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !resolvedAt.Valid || resolvedAt.String == "" {
		t.Fatal("expected resolved_at to be set after resolve")
	}

	// Store again — should be a regression
	result, err = s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if result.IsNew {
		t.Fatal("regression should not be marked as new")
	}
	if !result.IsRegression {
		t.Fatal("expected IsRegression=true after resolving and re-storing")
	}

	// Verify resolved_at is cleared
	if err := s.DB.QueryRow("SELECT resolved_at FROM errors WHERE fingerprint = ?", result.Fingerprint).Scan(&resolvedAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if resolvedAt.Valid && resolvedAt.String != "" {
		t.Fatal("expected resolved_at to be cleared after regression")
	}
}

func TestRegressionResolvedDuration(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type: "DurationErr", Value: "check duration",
				Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "d.go", Function: "f", Lineno: 1}}},
			}},
		},
	}

	// Store and resolve
	result, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Resolve and backdate resolved_at to 3 hours ago
	resolvedTime := time.Now().UTC().Add(-3 * time.Hour).Format(time.RFC3339)
	if _, err := s.DB.Exec("UPDATE errors SET resolved_at = ? WHERE fingerprint = ?", resolvedTime, result.Fingerprint); err != nil {
		t.Fatalf("backdate resolved_at: %v", err)
	}

	// Store again — should be a regression with duration ~3 hours
	result, err = s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if !result.IsRegression {
		t.Fatal("expected regression")
	}
	if result.ResolvedDuration < 2*time.Hour || result.ResolvedDuration > 4*time.Hour {
		t.Fatalf("expected ResolvedDuration ~3h, got %v", result.ResolvedDuration)
	}
}

func TestSilencePermanent(t *testing.T) {
	s := setupStore(t)

	fp := "abc123"
	if err := s.Silence(fp, nil, "noisy error"); err != nil {
		t.Fatalf("silence: %v", err)
	}

	if !s.IsSilenced(fp) {
		t.Fatal("expected fingerprint to be silenced")
	}

	// Unrelated fingerprint should not be silenced
	if s.IsSilenced("other") {
		t.Fatal("unrelated fingerprint should not be silenced")
	}
}

func TestSilenceWithExpiry(t *testing.T) {
	s := setupStore(t)

	fp := "expiring123"

	// Silence with expiry in the past
	past := time.Now().UTC().Add(-1 * time.Hour)
	if err := s.Silence(fp, &past, "already expired"); err != nil {
		t.Fatalf("silence: %v", err)
	}

	if s.IsSilenced(fp) {
		t.Fatal("expired silence should not be active")
	}
}

func TestSilenceWithFutureExpiry(t *testing.T) {
	s := setupStore(t)

	fp := "future123"
	future := time.Now().UTC().Add(24 * time.Hour)
	if err := s.Silence(fp, &future, "temporary"); err != nil {
		t.Fatalf("silence: %v", err)
	}

	if !s.IsSilenced(fp) {
		t.Fatal("future-expiry silence should be active")
	}
}

func TestUnsilence(t *testing.T) {
	s := setupStore(t)

	fp := "unsil123"
	if err := s.Silence(fp, nil, "temporary"); err != nil {
		t.Fatalf("silence: %v", err)
	}
	if !s.IsSilenced(fp) {
		t.Fatal("expected silenced")
	}

	if err := s.Unsilence(fp); err != nil {
		t.Fatalf("unsilence: %v", err)
	}
	if s.IsSilenced(fp) {
		t.Fatal("expected not silenced after unsilence")
	}
}

func TestListSilencesExcludesExpired(t *testing.T) {
	s := setupStore(t)

	// Add permanent silence
	if err := s.Silence("perm1", nil, "permanent"); err != nil {
		t.Fatalf("silence: %v", err)
	}

	// Add expired silence
	past := time.Now().UTC().Add(-1 * time.Hour)
	if err := s.Silence("expired1", &past, "old"); err != nil {
		t.Fatalf("silence: %v", err)
	}

	// Add future silence
	future := time.Now().UTC().Add(24 * time.Hour)
	if err := s.Silence("future1", &future, "soon"); err != nil {
		t.Fatalf("silence: %v", err)
	}

	entries, err := s.ListSilences()
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 active silences, got %d", len(entries))
	}

	// Verify no expired entries
	for _, e := range entries {
		if e.Fingerprint == "expired1" {
			t.Fatal("expired silence should not be in list")
		}
	}
}

func TestPruneExpiredSilences(t *testing.T) {
	s := setupStore(t)

	// Add permanent silence
	if err := s.Silence("perm1", nil, "permanent"); err != nil {
		t.Fatalf("silence: %v", err)
	}

	// Add expired silence
	past := time.Now().UTC().Add(-1 * time.Hour)
	if err := s.Silence("expired1", &past, "old"); err != nil {
		t.Fatalf("silence: %v", err)
	}

	pruned, err := s.PruneExpiredSilences()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}

	// Verify only permanent remains
	entries, err := s.ListSilences()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(entries))
	}
	if entries[0].Fingerprint != "perm1" {
		t.Fatalf("expected perm1, got %s", entries[0].Fingerprint)
	}
}

func TestNoResolvedDurationForNewError(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type: "FreshErr", Value: "brand new",
				Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "f.go", Function: "f", Lineno: 1}}},
			}},
		},
	}

	result, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if result.ResolvedDuration != 0 {
		t.Fatalf("expected zero ResolvedDuration for new error, got %v", result.ResolvedDuration)
	}
}
