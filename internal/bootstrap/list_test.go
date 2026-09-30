package bootstrap

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PhilHem/drillip/internal/adapter/in/api"
	store "github.com/PhilHem/drillip/internal/adapter/out/sqlite"
	"github.com/PhilHem/drillip/internal/application/service"
	"github.com/PhilHem/drillip/internal/domain"
)

func TestRootHelpDescribesAvailableCommands(t *testing.T) {
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"--help"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"list", "top", "recent", "show", "trend", "releases", "correlate", "stats", "resolve", "silence", "silences", "unsilence", "gc", "health", "serve", "maintenance"} {
		if !strings.Contains(output.String(), "\n  "+command+" ") {
			t.Errorf("help omits command %s: %s", command, &output)
		}
	}
	for _, description := range []string{"Browse and search", "total occurrence count", "first seen", "existing local database"} {
		if !strings.Contains(output.String(), description) {
			t.Errorf("help omits purpose %q: %s", description, &output)
		}
	}
}

func TestListPaginationHintsPreserveSelectedBackend(t *testing.T) {
	for _, mode := range []string{"server", "maintenance"} {
		t.Run(mode, func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "operator's data; literal.db")
			s, err := store.Open(db)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			fingerprints := make([]string, 0, 3)
			for _, message := range []string{"page first", "page second", "page third"} {
				event, err := s.StoreEvent(&domain.Event{Message: message, Tags: map[string]string{"service": "api's & workers"}})
				if err != nil {
					t.Fatal(err)
				}
				fingerprints = append(fingerprints, event.Fingerprint)
			}
			app := service.New(s, nil, nil)
			h := &api.Handler{Errors: app, Correlation: app}
			var calls atomic.Int32
			routes := h.Routes()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				routes.ServeHTTP(w, r)
			}))
			defer srv.Close()
			t.Setenv("DRILLIP_SERVER", "http://wrong-host.invalid:8300")
			t.Setenv("DRILLIP_DB", filepath.Join(t.TempDir(), "wrong.db"))
			prefix := []string{"--server", srv.URL}
			if mode == "maintenance" {
				prefix = []string{"maintenance", "--db", db}
			}
			args := append(prefix, "list", "--search", "PAGE", "--tag", "service=api's & workers", "--sort", "count", "--limit", "2")
			run := func(args []string) string {
				t.Helper()
				var output, stderr bytes.Buffer
				if err := Run(context.Background(), args, &output, &stderr); err != nil {
					t.Fatalf("%q: %v; stderr: %s", args, err, &stderr)
				}
				return output.String()
			}
			first := run(args)
			_, hint, ok := strings.Cut(first, " show <fingerprint>\n→ ")
			if !ok {
				t.Fatalf("missing next-page hint: %s", first)
			}
			// Copy the printed command through a shell, then run the resulting
			// argv against the real selected HTTP or local backend.
			parsed, err := exec.Command("sh", "-c", "drillip() { printf '%s\\000' \"$@\"; }\n"+hint).Output()
			if err != nil {
				t.Fatalf("copy page hint: %v", err)
			}
			second := run(strings.Split(strings.TrimSuffix(string(parsed), "\x00"), "\x00"))
			for _, fp := range fingerprints {
				if strings.Count(first+second, fp) != 1 {
					t.Fatalf("fingerprint %s missing or repeated across pages:\n%s\n%s", fp, first, second)
				}
			}
			if strings.Contains(second, " list --") {
				t.Fatalf("last page offered a next page: %s", second)
			}
			wantCalls := int32(4) // Each page probes capabilities before listing.
			if mode == "maintenance" {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Fatalf("HTTP requests = %d, want %d", calls.Load(), wantCalls)
			}
		})
	}
}
