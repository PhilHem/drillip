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
