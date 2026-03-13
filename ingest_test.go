package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	if err := initDB(dbPath); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
}

func TestHandleHealth(t *testing.T) {
	setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/-/healthy", nil)
	w := httptest.NewRecorder()
	handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandlePlainJSONEvent(t *testing.T) {
	setupTestDB(t)

	event := Event{
		EventID:     "test-123",
		Release:     "v1.0.0",
		Environment: "production",
		Platform:    "go",
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:  "RuntimeError",
				Value: "something broke",
				Stacktrace: &Stacktrace{
					Frames: []Frame{{
						Filename: "main.go",
						Function: "handleRequest",
						Lineno:   42,
					}},
				},
			}},
		},
		Breadcrumbs: &BreadcrumbData{
			Values: []json.RawMessage{
				json.RawMessage(`{"category":"http","message":"GET /api/data"}`),
			},
		},
	}

	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleIngest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify stored
	var count int
	if err := db.QueryRow("SELECT count FROM errors WHERE type = 'RuntimeError'").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	// Send again — count should increment
	req = httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w = httptest.NewRecorder()
	handleIngest(w, req)

	if err := db.QueryRow("SELECT count FROM errors WHERE type = 'RuntimeError'").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}
}

func TestHandleEnvelopeEvent(t *testing.T) {
	setupTestDB(t)

	event := Event{
		EventID: "env-456",
		Release: "v2.0.0",
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:  "ValueError",
				Value: "invalid input",
				Stacktrace: &Stacktrace{
					Frames: []Frame{{
						Filename: "app.py",
						Function: "validate",
						Lineno:   99,
					}},
				},
			}},
		},
	}

	eventJSON, _ := json.Marshal(event)
	envelope := `{"event_id":"env-456","dsn":"http://key@localhost:8300/1"}` + "\n" +
		`{"type":"event","length":` + fmt.Sprintf("%d", len(eventJSON)) + `}` + "\n" +
		string(eventJSON) + "\n"

	req := httptest.NewRequest(http.MethodPost, "/api/1/envelope/", bytes.NewReader([]byte(envelope)))
	w := httptest.NewRecorder()
	handleIngest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var typ, release string
	if err := db.QueryRow("SELECT type, release_tag FROM errors WHERE type = 'ValueError'").Scan(&typ, &release); err != nil {
		t.Fatalf("query: %v", err)
	}
	if typ != "ValueError" || release != "v2.0.0" {
		t.Fatalf("unexpected: type=%q release=%q", typ, release)
	}
}

func TestHandleEmptyEvent(t *testing.T) {
	setupTestDB(t)

	// No exception, no message — should be accepted but not stored
	body := []byte(`{"event_id":"no-content"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleIngest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM errors").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 errors stored, got %d", count)
	}
}

func TestHandleMethodNotAllowed(t *testing.T) {
	setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/1/store/", nil)
	w := httptest.NewRecorder()
	handleIngest(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestFingerprintGrouping(t *testing.T) {
	setupTestDB(t)

	// Same exception type + location = same fingerprint
	event1 := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:  "IOError",
				Value: "file not found: a.txt",
				Stacktrace: &Stacktrace{
					Frames: []Frame{{Filename: "io.go", Function: "readFile", Lineno: 10}},
				},
			}},
		},
	}
	event2 := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:  "IOError",
				Value: "file not found: b.txt", // different message, same location
				Stacktrace: &Stacktrace{
					Frames: []Frame{{Filename: "io.go", Function: "readFile", Lineno: 10}},
				},
			}},
		},
	}

	fp1 := fingerprint(&event1)
	fp2 := fingerprint(&event2)
	if fp1 != fp2 {
		t.Fatalf("same type+location should have same fingerprint: %q != %q", fp1, fp2)
	}

	// Different location = different fingerprint
	event3 := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:  "IOError",
				Value: "file not found",
				Stacktrace: &Stacktrace{
					Frames: []Frame{{Filename: "io.go", Function: "writeFile", Lineno: 20}},
				},
			}},
		},
	}
	fp3 := fingerprint(&event3)
	if fp1 == fp3 {
		t.Fatalf("different location should have different fingerprint")
	}
}

func TestOccurrenceInsertion(t *testing.T) {
	setupTestDB(t)

	event := Event{
		EventID: "occ-test",
		Release: "v3.0.0",
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:  "TestError",
				Value: "occ test",
				Stacktrace: &Stacktrace{
					Frames: []Frame{{Filename: "occ.go", Function: "doStuff", Lineno: 1}},
				},
			}},
		},
	}

	if err := storeEvent(&event); err != nil {
		t.Fatalf("store: %v", err)
	}

	var occCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&occCount); err != nil {
		t.Fatalf("query: %v", err)
	}
	if occCount != 1 {
		t.Fatalf("expected 1 occurrence, got %d", occCount)
	}

	// Send again — should get 2 occurrences
	if err := storeEvent(&event); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&occCount); err != nil {
		t.Fatalf("query: %v", err)
	}
	if occCount != 2 {
		t.Fatalf("expected 2 occurrences, got %d", occCount)
	}

	// Verify release_tag
	var release string
	if err := db.QueryRow("SELECT release_tag FROM occurrences LIMIT 1").Scan(&release); err != nil {
		t.Fatalf("query: %v", err)
	}
	if release != "v3.0.0" {
		t.Fatalf("expected release v3.0.0, got %q", release)
	}
}

func TestTraceIDExtraction(t *testing.T) {
	// No contexts
	ev1 := Event{}
	if got := ev1.TraceID(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}

	// With trace context
	ev2 := Event{
		Contexts: map[string]json.RawMessage{
			"trace": json.RawMessage(`{"trace_id":"abc123","span_id":"def456"}`),
		},
	}
	if got := ev2.TraceID(); got != "abc123" {
		t.Fatalf("expected abc123, got %q", got)
	}

	// Invalid JSON in trace context
	ev3 := Event{
		Contexts: map[string]json.RawMessage{
			"trace": json.RawMessage(`not json`),
		},
	}
	if got := ev3.TraceID(); got != "" {
		t.Fatalf("expected empty for invalid json, got %q", got)
	}
}

func TestOccurrenceTraceID(t *testing.T) {
	setupTestDB(t)

	event := Event{
		EventID: "trace-test",
		Release: "v1.0.0",
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:  "TraceError",
				Value: "trace test",
				Stacktrace: &Stacktrace{
					Frames: []Frame{{Filename: "t.go", Function: "fn", Lineno: 1}},
				},
			}},
		},
		Contexts: map[string]json.RawMessage{
			"trace": json.RawMessage(`{"trace_id":"deadbeef12345678"}`),
		},
	}

	if err := storeEvent(&event); err != nil {
		t.Fatalf("store: %v", err)
	}

	var traceID string
	if err := db.QueryRow("SELECT trace_id FROM occurrences LIMIT 1").Scan(&traceID); err != nil {
		t.Fatalf("query: %v", err)
	}
	if traceID != "deadbeef12345678" {
		t.Fatalf("expected deadbeef12345678, got %q", traceID)
	}
}

func TestHandleMessageEvent(t *testing.T) {
	setupTestDB(t)

	body := []byte(`{"event_id":"msg-1","level":"info","logentry":{"formatted":"Deployment started for v1.2.0","message":"Deployment started for %s"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleIngest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var typ, val, level string
	if err := db.QueryRow("SELECT type, value, level FROM errors WHERE type = 'message'").Scan(&typ, &val, &level); err != nil {
		t.Fatalf("query: %v", err)
	}
	if typ != "message" {
		t.Fatalf("expected type=message, got %q", typ)
	}
	if val != "Deployment started for v1.2.0" {
		t.Fatalf("expected formatted message, got %q", val)
	}
	if level != "info" {
		t.Fatalf("expected level=info, got %q", level)
	}
}

func TestHandleMessageWithPlainField(t *testing.T) {
	setupTestDB(t)

	// Some SDKs send "message" field instead of "logentry"
	body := []byte(`{"event_id":"msg-2","level":"warning","message":"disk space low"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleIngest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var val, level string
	if err := db.QueryRow("SELECT value, level FROM errors WHERE value = 'disk space low'").Scan(&val, &level); err != nil {
		t.Fatalf("query: %v", err)
	}
	if level != "warning" {
		t.Fatalf("expected level=warning, got %q", level)
	}
}

func TestMessageFingerprinting(t *testing.T) {
	// Same message = same fingerprint
	ev1 := Event{Message: "deploy started"}
	ev2 := Event{Message: "deploy started"}
	if fingerprint(&ev1) != fingerprint(&ev2) {
		t.Fatal("same message should produce same fingerprint")
	}

	// Different message = different fingerprint
	ev3 := Event{Message: "deploy finished"}
	if fingerprint(&ev1) == fingerprint(&ev3) {
		t.Fatal("different message should produce different fingerprint")
	}

	// Message vs exception = different fingerprint
	ev4 := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{Type: "Error", Value: "deploy started"}},
		},
	}
	if fingerprint(&ev1) == fingerprint(&ev4) {
		t.Fatal("message and exception with same text should differ")
	}
}

func TestExceptionLevelDefault(t *testing.T) {
	ev := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{Type: "RuntimeError", Value: "boom"}},
		},
	}
	if got := ev.EffectiveLevel(); got != "error" {
		t.Fatalf("expected error, got %q", got)
	}
}

func TestMessageLevelDefault(t *testing.T) {
	ev := Event{Message: "hello"}
	if got := ev.EffectiveLevel(); got != "info" {
		t.Fatalf("expected info, got %q", got)
	}
}

func TestExplicitLevelOverride(t *testing.T) {
	ev := Event{Level: "fatal", Exception: &ExceptionData{
		Values: []ExceptionValue{{Type: "OOM", Value: "out of memory"}},
	}}
	if got := ev.EffectiveLevel(); got != "fatal" {
		t.Fatalf("expected fatal, got %q", got)
	}
}

func TestEnvOverrides(t *testing.T) {
	// Just verify env vars are read (don't actually start server)
	os.Setenv("DRILLIP_DB", "/tmp/custom.db")
	os.Setenv("DRILLIP_ADDR", "0.0.0.0:9999")
	defer os.Unsetenv("DRILLIP_DB")
	defer os.Unsetenv("DRILLIP_ADDR")

	dbPath := "errors.db"
	addr := "127.0.0.1:8300"
	if v := os.Getenv("DRILLIP_DB"); v != "" {
		dbPath = v
	}
	if v := os.Getenv("DRILLIP_ADDR"); v != "" {
		addr = v
	}

	if dbPath != "/tmp/custom.db" {
		t.Fatalf("expected custom db path, got %q", dbPath)
	}
	if addr != "0.0.0.0:9999" {
		t.Fatalf("expected custom addr, got %q", addr)
	}
}
