package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/adapter/in/api"
	"github.com/PhilHem/drillip/internal/adapter/in/ingest"
	store "github.com/PhilHem/drillip/internal/adapter/out/sqlite"
	"github.com/PhilHem/drillip/internal/application/service"
	"github.com/PhilHem/drillip/internal/domain"
)

type serverTelemetry struct{}

func (serverTelemetry) Logs(_ context.Context, _ time.Time) ([]domain.JournalEntry, error) {
	return []domain.JournalEntry{{Message: "server-owned diagnostic"}}, nil
}
func (serverTelemetry) Trace(_ context.Context, _ string) (*domain.TraceData, error) {
	return &domain.TraceData{ServiceName: "checkout", Spans: []domain.TraceSpan{{OperationName: "charge", Duration: time.Millisecond}}}, nil
}
func (serverTelemetry) Metrics(_ context.Context, _ time.Time) (*domain.MetricsSnapshot, error) {
	return &domain.MetricsSnapshot{Values: map[string]string{"requests": "42"}}, nil
}
func (serverTelemetry) Profile(_ context.Context, _ time.Time) ([]domain.ProfileEntry, error) {
	return []domain.ProfileEntry{{Function: "checkout"}}, nil
}

func TestEveryNormalCommandUsesServer(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	app := service.New(s, nil, serverTelemetry{})
	h := &api.Handler{Errors: app, Correlation: app}
	mux := http.NewServeMux()
	mux.Handle("/api/0/", h.Routes())
	mux.HandleFunc("/-/healthy", ingest.HandleHealth(app))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("DRILLIP_SERVER", srv.URL)
	t.Setenv("DRILLIP_ADDR", "wrong-host.invalid:8300")
	t.Setenv("DRILLIP_DB", filepath.Join(t.TempDir(), "missing", "client.db"))
	event, err := s.StoreEvent(&domain.Event{Message: "command-mode-event", Release: "v1", Tags: map[string]string{"key": "a&b+c"}})
	if err != nil {
		t.Fatal(err)
	}
	fp := event.Fingerprint[:8]
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"list", "--search", "COMMAND-mode", "--tag", "key=a&b+c"}, fp},
		{[]string{"top", "--limit", "1"}, fp}, {[]string{"recent", "--tag", "key=a&b+c"}, fp},
		{[]string{"show", fp}, event.Fingerprint}, {[]string{"trend", fp}, "Trend"}, {[]string{"releases", fp}, "v1"},
		{[]string{"correlate", fp}, "server-owned diagnostic"}, {[]string{"stats"}, "Unique errors:      1"},
		{[]string{"silence", "--reason", "planned & test", fp, "1h"}, "silenced"}, {[]string{"silences"}, "planned & test"},
		{[]string{"unsilence", fp}, "unsilenced"}, {[]string{"resolve", fp}, "resolved"}, {[]string{"gc", "100w"}, "deleted 0"}, {[]string{"health"}, "ok"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, errout bytes.Buffer
			if err := Run(context.Background(), tc.args, &out, &errout); err != nil || !strings.Contains(out.String(), tc.want) {
				t.Fatalf("err=%v output=%s stderr=%s", err, &out, &errout)
			}
		})
	}
}
func TestHelpAndInvalidInputNeverAccessBackend(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	t.Setenv("DRILLIP_SERVER", srv.URL)
	db := filepath.Join(t.TempDir(), "never-created.db")
	t.Setenv("DRILLIP_DB", db)
	for _, args := range [][]string{
		{"--help"}, {"serve", "--help"}, {"show", "--help"}, {"top", "--help"}, {"health", "--help"}, {"list", "--help"},
		{"list", "--limit", "0"}, {"list", "--limit", "501"}, {"list", "--offset", "-1"}, {"list", "--sort", "invalid"}, {"list", "--tag", "invalid"},
		{"unknown"}, {"show"}, {"show", "bad!"}, {"show", "abcd", "extra"}, {"top", "--limit", "0"}, {"recent", "--hours", "-1"}, {"silence", "abcd", "bad-duration"}, {"gc", "nonsense"}, {"stats", "unexpected"}, {"health", "unexpected"},
	} {
		var out, errout bytes.Buffer
		_ = Run(context.Background(), args, &out, &errout)
	}
	if calls.Load() != 0 {
		t.Fatalf("made %d requests", calls.Load())
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatalf("database created: %v", err)
	}
}
func TestRemovedMaintenanceNeverAccessesBackend(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	t.Setenv("DRILLIP_SERVER", srv.URL)
	db := filepath.Join(t.TempDir(), "local.db")
	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	event, err := s.StoreEvent(&domain.Event{Message: "keep unchanged"})
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "never-created.db")
	t.Setenv("DRILLIP_DB", missing)
	for _, args := range [][]string{
		{"maintenance"}, {"maintenance", "--help"},
		{"maintenance", "--db", db, "show", event.Fingerprint},
		{"maintenance", "--db", db, "resolve", event.Fingerprint},
		{"maintenance", "--db", db, "silence", event.Fingerprint},
		{"maintenance", "--db", db, "gc", "1h"},
		{"maintenance", "--db", missing, "list"},
		{"maintenance", "--db", db, "backup", "--output", missing},
		{"--server", srv.URL, "maintenance", "--db", db, "resolve", event.Fingerprint},
		{"--db", db, "maintenance", "show", event.Fingerprint},
		{"--offline", "--db", db, "resolve", event.Fingerprint},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, errout bytes.Buffer
			err := Run(context.Background(), args, &out, &errout)
			if err == nil || !strings.Contains(err.Error(), "was removed") || !strings.Contains(err.Error(), "serve --db PATH") || !strings.Contains(err.Error(), "--server URL COMMAND") {
				t.Fatalf("missing migration instructions: %v", err)
			}
			if out.Len() != 0 {
				t.Fatalf("removed command wrote stdout: %s", &out)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("removed command made %d requests", calls.Load())
	}
	after, err := os.ReadFile(db)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("removed command changed the database: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("removed command created a file: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(db))
	if err != nil || len(entries) != 1 || entries[0].Name() != "local.db" {
		t.Fatalf("removed command left database sidecars: %v, %v", entries, err)
	}
}
func TestClientTargetPrecedencePreservesLegacyAddress(t *testing.T) {
	var first, second atomic.Int32
	server := func(counter *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { counter.Add(1); fmt.Fprint(w, "ok") }))
	}
	a, b := server(&first), server(&second)
	defer a.Close()
	defer b.Close()
	t.Setenv("DRILLIP_ADDR", strings.TrimPrefix(a.URL, "http://"))
	t.Setenv("DRILLIP_SERVER", "")
	runHealth := func(args ...string) {
		t.Helper()
		var out bytes.Buffer
		if err := Run(context.Background(), args, &out, &out); err != nil {
			t.Fatal(err)
		}
	}
	runHealth("health")
	if first.Load() != 1 {
		t.Fatal("legacy environment target ignored")
	}
	t.Setenv("DRILLIP_SERVER", b.URL)
	runHealth("health")
	if second.Load() != 1 {
		t.Fatal("server environment did not override legacy")
	}
	runHealth("--server", a.URL, "health")
	if first.Load() != 2 {
		t.Fatal("explicit server did not override environment")
	}
	runHealth("--addr", strings.TrimPrefix(a.URL, "http://"), "health")
	if first.Load() != 3 {
		t.Fatal("explicit legacy address did not override environment")
	}
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"--server", a.URL, "--addr", "other:8300", "health"}, &out, &out); err == nil {
		t.Fatal("accepted conflicting targets")
	}
}
