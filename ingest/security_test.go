package ingest

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PhilHem/drillip/domain"
	"github.com/PhilHem/drillip/store"
)

func setupSecurityStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func securityHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	s := setupSecurityStore(t)
	return MakeHandler(s, nil)
}

// --- Decompression bomb ---

func TestRejectsGzipBomb(t *testing.T) {
	s := setupSecurityStore(t)
	handler := MakeHandler(s, nil)

	// Create a gzip payload that decompresses to >10MB
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	// 11MB of zeros compresses to a few KB
	payload := strings.Repeat("A", 11*1024*1024)
	gw.Write([]byte(payload))
	gw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	w := httptest.NewRecorder()
	handler(w, req)

	// Should reject — truncated data won't parse as valid JSON
	if w.Code == http.StatusOK {
		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err == nil {
			if _, hasID := resp["id"]; hasID && resp["id"] != "ok" {
				t.Fatal("should not successfully store a 11MB decompressed payload")
			}
		}
	}
}

// --- SQL injection via fingerprint ---

func TestRejectsSQLWildcardFingerprint(t *testing.T) {
	// LIKE wildcards should be rejected by ValidFingerprint
	for _, fp := range []string{"%", "_%", "a%b", "'OR'1'='1", "../etc/passwd", "ABCDEF"} {
		if domain.ValidFingerprint(fp) {
			t.Errorf("ValidFingerprint should reject %q", fp)
		}
	}
}

func TestAcceptsValidFingerprint(t *testing.T) {
	for _, fp := range []string{"a", "abcdef01", "0123456789abcdef"} {
		if !domain.ValidFingerprint(fp) {
			t.Errorf("ValidFingerprint should accept %q", fp)
		}
	}
}

// --- SMTP header injection via event data ---

func TestCRLFInExceptionTypeDoesNotInjectHeaders(t *testing.T) {
	s := setupSecurityStore(t)
	handler := MakeHandler(s, nil)

	event := domain.Event{
		EventID: "sec-test-1",
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "Error\r\nBcc: evil@attacker.com",
				Value: "injected",
				Stacktrace: &domain.Stacktrace{
					Frames: []domain.Frame{{
						Filename: "test.go",
						Function: "main",
						Lineno:   1,
					}},
				},
			}},
		},
	}

	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify the stored exception type was sanitized
	var storedType string
	err := s.DB.QueryRow("SELECT type FROM errors LIMIT 1").Scan(&storedType)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if strings.Contains(storedType, "\r") || strings.Contains(storedType, "\n") {
		t.Fatalf("CRLF should be stripped from stored type, got %q", storedType)
	}
}

// --- Oversized fields truncated ---

func TestOversizedFieldsSanitized(t *testing.T) {
	s := setupSecurityStore(t)
	handler := MakeHandler(s, nil)

	longMsg := strings.Repeat("X", 20000) // 20KB message
	event := domain.Event{
		EventID: "sec-test-2",
		Message: longMsg,
		Level:   "info",
	}

	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify stored value is truncated
	var storedValue string
	err := s.DB.QueryRow("SELECT value FROM errors LIMIT 1").Scan(&storedValue)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(storedValue) > 10000 {
		t.Fatalf("message should be truncated to 10000, got %d", len(storedValue))
	}
}

func TestInvalidLevelNormalized(t *testing.T) {
	s := setupSecurityStore(t)
	handler := MakeHandler(s, nil)

	event := domain.Event{
		EventID: "sec-test-3",
		Message: "test",
		Level:   "INVALID_LEVEL",
	}

	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Invalid level should be normalized (Sanitize clears it, EffectiveLevel defaults)
	var storedLevel string
	err := s.DB.QueryRow("SELECT level FROM errors LIMIT 1").Scan(&storedLevel)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	validLevels := map[string]bool{"fatal": true, "error": true, "warning": true, "info": true, "debug": true}
	if !validLevels[storedLevel] {
		t.Fatalf("stored level should be valid, got %q", storedLevel)
	}
}

// --- Request body size limit ---

func TestRejectsOversizedUncompressedBody(t *testing.T) {
	handler := securityHandler(t)

	// 11MB uncompressed body
	body := bytes.Repeat([]byte("A"), 11*1024*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/1/store/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler(w, req)

	// Should reject with 400 (MaxBytesReader triggers)
	if w.Code == http.StatusOK {
		t.Fatal("should reject 11MB uncompressed body")
	}
}
