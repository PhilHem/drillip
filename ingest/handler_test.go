package ingest

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PhilHem/drillip/domain"
	"github.com/PhilHem/drillip/notify"
	"github.com/PhilHem/drillip/store"
	"github.com/andybalholm/brotli"
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

func TestHandleHealth(t *testing.T) {
	s := setupStore(t)
	req := httptest.NewRequest(http.MethodGet, "/-/healthy", nil)
	w := httptest.NewRecorder()
	HandleHealth(s.DB)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandlePlainJSONEvent(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		EventID:     "test-123",
		Release:     "v1.0.0",
		Environment: "production",
		Platform:    "go",
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "RuntimeError",
				Value: "something broke",
				Stacktrace: &domain.Stacktrace{
					Frames: []domain.Frame{{
						Filename: "main.go",
						Function: "handleRequest",
						Lineno:   42,
					}},
				},
			}},
		},
		Breadcrumbs: &domain.BreadcrumbData{
			Values: []json.RawMessage{
				json.RawMessage(`{"category":"http","message":"GET /api/data"}`),
			},
		},
	}

	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	MakeHandler(s, nil)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify stored
	var count int
	if err := s.DB.QueryRow("SELECT count FROM errors WHERE type = 'RuntimeError'").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	// Send again — count should increment
	req = httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w = httptest.NewRecorder()
	MakeHandler(s, nil)(w, req)

	if err := s.DB.QueryRow("SELECT count FROM errors WHERE type = 'RuntimeError'").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}
}

func TestHandleEnvelopeEvent(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		EventID: "env-456",
		Release: "v2.0.0",
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "ValueError",
				Value: "invalid input",
				Stacktrace: &domain.Stacktrace{
					Frames: []domain.Frame{{
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
	MakeHandler(s, nil)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var typ, release string
	if err := s.DB.QueryRow("SELECT type, release_tag FROM errors WHERE type = 'ValueError'").Scan(&typ, &release); err != nil {
		t.Fatalf("query: %v", err)
	}
	if typ != "ValueError" || release != "v2.0.0" {
		t.Fatalf("unexpected: type=%q release=%q", typ, release)
	}
}

func TestHandleBrotliEnvelope(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		EventID: "br-789",
		Release: "v3.0.0",
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "BrotliError",
				Value: "compressed payload",
			}},
		},
	}

	eventJSON, _ := json.Marshal(event)
	envelope := `{"event_id":"br-789","dsn":"http://key@localhost:8300/1"}` + "\n" +
		`{"type":"event","content_type":"application/json","length":` + fmt.Sprintf("%d", len(eventJSON)) + `}` + "\n" +
		string(eventJSON) + "\n"

	var buf bytes.Buffer
	bw := brotli.NewWriter(&buf)
	bw.Write([]byte(envelope))
	bw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/1/envelope/", &buf)
	req.Header.Set("Content-Encoding", "br")
	w := httptest.NewRecorder()
	MakeHandler(s, nil)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var typ string
	if err := s.DB.QueryRow("SELECT type FROM errors WHERE type = 'BrotliError'").Scan(&typ); err != nil {
		t.Fatalf("query: %v", err)
	}
	if typ != "BrotliError" {
		t.Fatalf("expected BrotliError, got %q", typ)
	}
}

func TestHandleGzipEnvelope(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		EventID: "gz-789",
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "GzipError",
				Value: "compressed payload",
			}},
		},
	}

	eventJSON, _ := json.Marshal(event)
	envelope := `{"event_id":"gz-789","dsn":"http://key@localhost:8300/1"}` + "\n" +
		`{"type":"event","content_type":"application/json","length":` + fmt.Sprintf("%d", len(eventJSON)) + `}` + "\n" +
		string(eventJSON) + "\n"

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte(envelope))
	gw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/1/envelope/", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	w := httptest.NewRecorder()
	MakeHandler(s, nil)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var typ string
	if err := s.DB.QueryRow("SELECT type FROM errors WHERE type = 'GzipError'").Scan(&typ); err != nil {
		t.Fatalf("query: %v", err)
	}
	if typ != "GzipError" {
		t.Fatalf("expected GzipError, got %q", typ)
	}
}

func TestHandleEmptyEvent(t *testing.T) {
	s := setupStore(t)

	// No exception, no message — should be accepted but not stored
	body := []byte(`{"event_id":"no-content"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	MakeHandler(s, nil)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var count int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM errors").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 errors stored, got %d", count)
	}
}

func TestHandleMethodNotAllowed(t *testing.T) {
	s := setupStore(t)
	req := httptest.NewRequest(http.MethodGet, "/api/1/store/", nil)
	w := httptest.NewRecorder()
	MakeHandler(s, nil)(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleMessageEvent(t *testing.T) {
	s := setupStore(t)

	body := []byte(`{"event_id":"msg-1","level":"info","logentry":{"formatted":"Deployment started for v1.2.0","message":"Deployment started for %s"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	MakeHandler(s, nil)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var typ, val, level string
	if err := s.DB.QueryRow("SELECT type, value, level FROM errors WHERE type = 'message'").Scan(&typ, &val, &level); err != nil {
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
	s := setupStore(t)

	// Some SDKs send "message" field instead of "logentry"
	body := []byte(`{"event_id":"msg-2","level":"warning","message":"disk space low"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	MakeHandler(s, nil)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var val, level string
	if err := s.DB.QueryRow("SELECT value, level FROM errors WHERE value = 'disk space low'").Scan(&val, &level); err != nil {
		t.Fatalf("query: %v", err)
	}
	if level != "warning" {
		t.Fatalf("expected level=warning, got %q", level)
	}
}

func TestIngestReturnsFingerprint(t *testing.T) {
	s := setupStore(t)

	body := []byte(`{"event_id":"fp-test","level":"error","message":"test fingerprint return"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	MakeHandler(s, nil)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	fp, ok := resp["id"]
	if !ok || fp == "" || fp == "ok" {
		t.Fatalf("expected fingerprint in response, got %q", fp)
	}

	// Verify the returned fingerprint matches what's in the DB
	var count int
	if err := s.DB.QueryRow("SELECT count FROM errors WHERE fingerprint = ?", fp).Scan(&count); err != nil {
		t.Fatalf("fingerprint %q not found in DB: %v", fp, err)
	}
}

func TestRegressionTriggersNotification(t *testing.T) {
	s := setupStore(t)

	n := notify.NewNotifier(notify.SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	var notified int32
	n.SetSendMail(func(_ string, _ smtp.Auth, _ string, _ []string, _ []byte) error {
		atomic.AddInt32(&notified, 1)
		return nil
	})

	event := domain.Event{
		EventID: "reg-test",
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "RegError",
				Value: "regression test",
				Stacktrace: &domain.Stacktrace{
					Frames: []domain.Frame{{Filename: "r.go", Function: "f", Lineno: 1}},
				},
			}},
		},
	}

	// First ingest — new error, should notify
	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	MakeHandler(s, n)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Wait briefly for the goroutine to complete
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&notified) != 1 {
		t.Fatalf("expected 1 notification for new error, got %d", atomic.LoadInt32(&notified))
	}

	// Get fingerprint to resolve it
	var fp string
	if err := s.DB.QueryRow("SELECT fingerprint FROM errors WHERE type = 'RegError'").Scan(&fp); err != nil {
		t.Fatalf("query fp: %v", err)
	}
	if _, err := s.Resolve(fp); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Second ingest — regression, should notify again
	body, _ = json.Marshal(event)
	req = httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w = httptest.NewRecorder()
	MakeHandler(s, n)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&notified) != 2 {
		t.Fatalf("expected 2 notifications (new + regression), got %d", atomic.LoadInt32(&notified))
	}

	// Third ingest — neither new nor regression, should NOT notify
	body, _ = json.Marshal(event)
	req = httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w = httptest.NewRecorder()
	MakeHandler(s, n)(w, req)

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&notified) != 2 {
		t.Fatalf("expected still 2 notifications (third ingest is neither new nor regression), got %d", atomic.LoadInt32(&notified))
	}
}
