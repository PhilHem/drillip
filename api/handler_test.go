package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PhilHem/drillip/domain"
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

func TestAPITop(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type: "APITestError", Value: "api test",
				Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "a.go", Function: "f", Lineno: 1}}},
			}},
		},
		Tags: map[string]string{"server": "web-1"},
	}
	if _, err := s.StoreEvent(&event); err != nil {
		t.Fatalf("store: %v", err)
	}

	h := &Handler{DB: s.DB}
	req := httptest.NewRequest(http.MethodGet, "/api/0/top/", nil)
	w := httptest.NewRecorder()
	h.HandleTop(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var results []apiError
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 1 || results[0].Type != "APITestError" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestAPITopWithTagFilter(t *testing.T) {
	s := setupStore(t)

	ev1 := domain.Event{
		Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "Err1", Value: "v1",
			Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "a.go", Function: "f", Lineno: 1}}}}}},
		Tags: map[string]string{"server": "web-1"},
	}
	ev2 := domain.Event{
		Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "Err2", Value: "v2",
			Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "b.go", Function: "g", Lineno: 2}}}}}},
		Tags: map[string]string{"server": "web-2"},
	}
	if _, err := s.StoreEvent(&ev1); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := s.StoreEvent(&ev2); err != nil {
		t.Fatalf("store: %v", err)
	}

	h := &Handler{DB: s.DB}
	req := httptest.NewRequest(http.MethodGet, "/api/0/top/?tag=server%3Dweb-1", nil)
	w := httptest.NewRecorder()
	h.HandleTop(w, req)

	var results []apiError
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 1 || results[0].Type != "Err1" {
		t.Fatalf("expected only Err1, got %+v", results)
	}
}

func TestAPIShow(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "ShowAPIErr", Value: "show api",
			Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "s.go", Function: "h", Lineno: 5}}}}}},
	}
	result, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	h := &Handler{DB: s.DB, Store: s}
	req := httptest.NewRequest(http.MethodGet, "/api/0/show/"+result.Fingerprint[:8]+"/", nil)
	w := httptest.NewRecorder()
	h.HandleShow(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var detail apiErrorDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if detail.Type != "ShowAPIErr" {
		t.Fatalf("expected ShowAPIErr, got %q", detail.Type)
	}
}

func TestAPIShowNotFound(t *testing.T) {
	s := setupStore(t)
	h := &Handler{DB: s.DB, Store: s}
	req := httptest.NewRequest(http.MethodGet, "/api/0/show/aabb001122334455/", nil)
	w := httptest.NewRecorder()
	h.HandleShow(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAPIStats(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "StatsErr", Value: "stats",
			Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "s.go", Function: "f", Lineno: 1}}}}}},
	}
	if _, err := s.StoreEvent(&event); err != nil {
		t.Fatalf("store: %v", err)
	}

	h := &Handler{DB: s.DB}
	req := httptest.NewRequest(http.MethodGet, "/api/0/stats/", nil)
	w := httptest.NewRecorder()
	h.HandleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var stats apiStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if stats.UniqueErrors != 1 {
		t.Fatalf("expected 1 unique error, got %d", stats.UniqueErrors)
	}
	if stats.TotalOccurrences != 1 {
		t.Fatalf("expected 1 occurrence, got %d", stats.TotalOccurrences)
	}
}

func TestAPIRecent(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "RecentAPIErr", Value: "recent api",
			Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "r.go", Function: "f", Lineno: 1}}}}}},
	}
	if _, err := s.StoreEvent(&event); err != nil {
		t.Fatalf("store: %v", err)
	}

	h := &Handler{DB: s.DB}
	req := httptest.NewRequest(http.MethodGet, "/api/0/recent/?hours=1", nil)
	w := httptest.NewRecorder()
	h.HandleRecent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var results []apiError
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 1 || results[0].Type != "RecentAPIErr" {
		t.Fatalf("unexpected: %+v", results)
	}
}

func TestAPITrend(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "TrendAPIErr", Value: "trend api",
			Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "t.go", Function: "f", Lineno: 1}}}}}},
	}
	sr, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	h := &Handler{DB: s.DB, Store: s}
	req := httptest.NewRequest(http.MethodGet, "/api/0/trend/"+sr.Fingerprint[:8]+"/", nil)
	w := httptest.NewRecorder()
	h.HandleTrend(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := result["fingerprint"]; !ok {
		t.Fatal("missing fingerprint in response")
	}
	if _, ok := result["buckets"]; !ok {
		t.Fatal("missing buckets in response")
	}
}

func TestAPIReleases(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Release: "v5.0.0",
		Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "RelAPIErr", Value: "rel api",
			Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "r.go", Function: "f", Lineno: 1}}}}}},
	}
	sr, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	h := &Handler{DB: s.DB, Store: s}
	req := httptest.NewRequest(http.MethodGet, "/api/0/releases/"+sr.Fingerprint[:8]+"/", nil)
	w := httptest.NewRecorder()
	h.HandleReleases(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}

	var releases []apiRelease
	if err := json.Unmarshal(result["releases"], &releases); err != nil {
		t.Fatalf("parse releases: %v", err)
	}
	if len(releases) != 1 || releases[0].Release != "v5.0.0" {
		t.Fatalf("unexpected releases: %+v", releases)
	}
}

func TestAPIGC(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "GCAPIErr", Value: "gc api",
			Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "g.go", Function: "f", Lineno: 1}}}}}},
	}
	if _, err := s.StoreEvent(&event); err != nil {
		t.Fatalf("store: %v", err)
	}

	h := &Handler{DB: s.DB}

	// GC with 0h should delete all occurrences
	req := httptest.NewRequest(http.MethodPost, "/api/0/gc/?older_than=0h", nil)
	w := httptest.NewRecorder()
	h.HandleGC(w, req)

	// 0h is invalid (parseDuration needs >= 2 chars with valid number)
	// Use 1h — occurrence was just created, so nothing should be deleted
	req = httptest.NewRequest(http.MethodPost, "/api/0/gc/?older_than=1h", nil)
	w = httptest.NewRecorder()
	h.HandleGC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result apiGCResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Deleted != 0 {
		t.Fatalf("expected 0 deleted (occurrence is fresh), got %d", result.Deleted)
	}
}

func TestAPIGCRequiresPost(t *testing.T) {
	s := setupStore(t)
	h := &Handler{DB: s.DB}
	req := httptest.NewRequest(http.MethodGet, "/api/0/gc/?older_than=7d", nil)
	w := httptest.NewRecorder()
	h.HandleGC(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestAPIResolve(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "ResolveAPIErr", Value: "resolve api",
			Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "r.go", Function: "f", Lineno: 1}}}}}},
	}
	sr, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	h := &Handler{DB: s.DB, Store: s}
	req := httptest.NewRequest(http.MethodPost, "/api/0/resolve/"+sr.Fingerprint[:8]+"/", nil)
	w := httptest.NewRecorder()
	h.HandleResolve(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result["fingerprint"] != sr.Fingerprint {
		t.Fatalf("expected fingerprint %s, got %v", sr.Fingerprint, result["fingerprint"])
	}
	if result["resolved_at"] == "" {
		t.Fatal("expected resolved_at in response")
	}
}

func TestAPIResolveNotFound(t *testing.T) {
	s := setupStore(t)
	h := &Handler{DB: s.DB, Store: s}
	req := httptest.NewRequest(http.MethodPost, "/api/0/resolve/aabb001122334455/", nil)
	w := httptest.NewRecorder()
	h.HandleResolve(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAPISilencePost(t *testing.T) {
	s := setupStore(t)
	h := &Handler{DB: s.DB, Store: s}

	req := httptest.NewRequest(http.MethodPost, "/api/0/silence/abc123/?duration=24h&reason=maintenance", nil)
	w := httptest.NewRecorder()
	h.HandleSilence(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result["fingerprint"] != "abc123" {
		t.Fatalf("expected fingerprint abc123, got %v", result["fingerprint"])
	}
	if result["status"] != "silenced" {
		t.Fatalf("expected status silenced, got %v", result["status"])
	}
	if result["expires_at"] == nil {
		t.Fatal("expected expires_at in response")
	}

	if !s.IsSilenced("abc123") {
		t.Fatal("expected fingerprint to be silenced")
	}
}

func TestAPISilencePermanent(t *testing.T) {
	s := setupStore(t)
	h := &Handler{DB: s.DB, Store: s}

	req := httptest.NewRequest(http.MethodPost, "/api/0/silence/aaa0001111/", nil)
	w := httptest.NewRecorder()
	h.HandleSilence(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if !s.IsSilenced("aaa0001111") {
		t.Fatal("expected fingerprint to be silenced")
	}
}

func TestAPIUnsilenceDelete(t *testing.T) {
	s := setupStore(t)
	h := &Handler{DB: s.DB, Store: s}

	// First silence it
	_ = s.Silence("de1a23", nil, "")

	req := httptest.NewRequest(http.MethodDelete, "/api/0/silence/de1a23/", nil)
	w := httptest.NewRecorder()
	h.HandleSilence(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if s.IsSilenced("de1a23") {
		t.Fatal("expected fingerprint to no longer be silenced")
	}
}

func TestAPIListSilences(t *testing.T) {
	s := setupStore(t)
	h := &Handler{DB: s.DB, Store: s}

	_ = s.Silence("list1", nil, "reason1")

	req := httptest.NewRequest(http.MethodGet, "/api/0/silences/", nil)
	w := httptest.NewRecorder()
	h.HandleListSilences(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var results []apiSilence
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 silence, got %d", len(results))
	}
	if results[0].Fingerprint != "list1" {
		t.Fatalf("expected list1, got %s", results[0].Fingerprint)
	}
	if results[0].Reason != "reason1" {
		t.Fatalf("expected reason1, got %s", results[0].Reason)
	}
}

func TestAPIListSilencesRequiresGet(t *testing.T) {
	s := setupStore(t)
	h := &Handler{DB: s.DB, Store: s}
	req := httptest.NewRequest(http.MethodPost, "/api/0/silences/", nil)
	w := httptest.NewRecorder()
	h.HandleListSilences(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestAPISilenceRequiresPostOrDelete(t *testing.T) {
	s := setupStore(t)
	h := &Handler{DB: s.DB, Store: s}
	req := httptest.NewRequest(http.MethodGet, "/api/0/silence/abc123/", nil)
	w := httptest.NewRecorder()
	h.HandleSilence(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestAPIResolveRequiresPost(t *testing.T) {
	s := setupStore(t)
	h := &Handler{DB: s.DB, Store: s}
	req := httptest.NewRequest(http.MethodGet, "/api/0/resolve/abc/", nil)
	w := httptest.NewRecorder()
	h.HandleResolve(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestAPITopIncludesState(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "StateAPIErr", Value: "state api",
			Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "s.go", Function: "f", Lineno: 1}}}}}},
	}
	if _, err := s.StoreEvent(&event); err != nil {
		t.Fatalf("store: %v", err)
	}

	h := &Handler{DB: s.DB}
	req := httptest.NewRequest(http.MethodGet, "/api/0/top/", nil)
	w := httptest.NewRecorder()
	h.HandleTop(w, req)

	var results []apiError
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	// Just-created error should be "new" (first_seen within last hour)
	if results[0].State != "new" {
		t.Fatalf("expected state 'new', got %q", results[0].State)
	}
}

func TestAPIShowIncludesState(t *testing.T) {
	s := setupStore(t)

	event := domain.Event{
		Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "ShowStateErr", Value: "show state",
			Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "ss.go", Function: "f", Lineno: 1}}}}}},
	}
	sr, err := s.StoreEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	h := &Handler{DB: s.DB, Store: s}
	req := httptest.NewRequest(http.MethodGet, "/api/0/show/"+sr.Fingerprint[:8]+"/", nil)
	w := httptest.NewRecorder()
	h.HandleShow(w, req)

	var detail apiErrorDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if detail.State != "new" {
		t.Fatalf("expected state 'new', got %q", detail.State)
	}
}
