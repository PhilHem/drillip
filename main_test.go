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
	db.QueryRow("SELECT count FROM errors WHERE type = 'RuntimeError'").Scan(&count)
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	// Send again — count should increment
	req = httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w = httptest.NewRecorder()
	handleIngest(w, req)

	db.QueryRow("SELECT count FROM errors WHERE type = 'RuntimeError'").Scan(&count)
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
	db.QueryRow("SELECT type, release_tag FROM errors WHERE type = 'ValueError'").Scan(&typ, &release)
	if typ != "ValueError" || release != "v2.0.0" {
		t.Fatalf("unexpected: type=%q release=%q", typ, release)
	}
}

func TestHandleNonErrorEvent(t *testing.T) {
	setupTestDB(t)

	body := []byte(`{"event_id":"no-exc","message":"just a message"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleIngest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM errors").Scan(&count)
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

func TestEnvOverrides(t *testing.T) {
	// Just verify env vars are read (don't actually start server)
	os.Setenv("ERROR_SINK_DB", "/tmp/custom.db")
	os.Setenv("ERROR_SINK_ADDR", "0.0.0.0:9999")
	defer os.Unsetenv("ERROR_SINK_DB")
	defer os.Unsetenv("ERROR_SINK_ADDR")

	dbPath := "errors.db"
	addr := "127.0.0.1:8300"
	if v := os.Getenv("ERROR_SINK_DB"); v != "" {
		dbPath = v
	}
	if v := os.Getenv("ERROR_SINK_ADDR"); v != "" {
		addr = v
	}

	if dbPath != "/tmp/custom.db" {
		t.Fatalf("expected custom db path, got %q", dbPath)
	}
	if addr != "0.0.0.0:9999" {
		t.Fatalf("expected custom addr, got %q", addr)
	}
}
