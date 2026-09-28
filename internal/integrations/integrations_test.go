package integrations

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestQueryJournalctlSkipNonLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("journalctl only available on Linux")
	}
}

func TestQueryJournalctlEmptyUnit(t *testing.T) {
	entries, err := QueryJournalctl("", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries != nil {
		t.Fatalf("expected nil for empty unit")
	}
}

func TestQueryVictoriaTracesEmptyURL(t *testing.T) {
	td, err := QueryVictoriaTraces("", "abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if td != nil {
		t.Fatalf("expected nil for empty URL")
	}
}

func TestQueryVictoriaTracesEmptyTraceID(t *testing.T) {
	td, err := QueryVictoriaTraces("http://example.com", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if td != nil {
		t.Fatalf("expected nil for empty trace ID")
	}
}

func TestQueryVictoriaTracesMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [{
				"processes": {"p1": {"serviceName": "myapp"}},
				"spans": [
					{"operationName": "GET /api", "duration": 1500, "tags": [{"key":"http.status","value":"200"}]}
				]
			}]
		}`))
	}))
	defer srv.Close()

	td, err := QueryVictoriaTraces(srv.URL, "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if td == nil {
		t.Fatal("expected trace data")
	}
	if td.ServiceName != "myapp" {
		t.Fatalf("expected myapp, got %q", td.ServiceName)
	}
	if len(td.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(td.Spans))
	}
	if td.Spans[0].OperationName != "GET /api" {
		t.Fatalf("expected GET /api, got %q", td.Spans[0].OperationName)
	}
}

func TestQueryVictoriaTraces500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	_, err := QueryVictoriaTraces(srv.URL, "abc")
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestQueryVictoriaMetricsEmptyURL(t *testing.T) {
	snap, err := QueryVictoriaMetrics("", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap != nil {
		t.Fatalf("expected nil for empty URL")
	}
}

func TestQueryVictoriaMetricsMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"result":[{"value":[1234567890,"0.42"]}]}}`))
	}))
	defer srv.Close()

	snap, err := QueryVictoriaMetrics(srv.URL, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	if len(snap.Values) == 0 {
		t.Fatal("expected some values")
	}
}

func TestQueryPyroscopeEmptyURL(t *testing.T) {
	entries, err := QueryPyroscope("", "svc", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries != nil {
		t.Fatalf("expected nil for empty URL")
	}
}

func TestQueryPyroscopeEmptyService(t *testing.T) {
	entries, err := QueryPyroscope("http://example.com", "", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries != nil {
		t.Fatalf("expected nil for empty service")
	}
}

func TestQueryPyroscopeMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"flamebearer":{"names":["main","doWork","allocMem"],"levels":[[0,100,50,0]]}}`))
	}))
	defer srv.Close()

	entries, err := QueryPyroscope(srv.URL, "myapp", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Function != "main" {
		t.Fatalf("expected main, got %q", entries[0].Function)
	}
}

func TestQueryPyroscope500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	_, err := QueryPyroscope(srv.URL, "myapp", time.Now())
	if err == nil {
		t.Fatal("expected error on 500")
	}
}
