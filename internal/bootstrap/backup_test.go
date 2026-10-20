package bootstrap

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PhilHem/drillip/internal/adapter/in/api"
	store "github.com/PhilHem/drillip/internal/adapter/out/sqlite"
	"github.com/PhilHem/drillip/internal/domain"
)

func TestBackupCLIProducesStandaloneDatabase(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	event, err := s.StoreEvent(&domain.Event{Message: "online backup", Tags: map[string]string{"service": "checkout"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Silence(event.Fingerprint, nil, "preserve"); err != nil {
		t.Fatal(err)
	}
	routes := (&api.Handler{Backups: s}).Routes()
	srv := httptest.NewServer(http.StripPrefix("/tracker", routes))
	defer srv.Close()
	t.Setenv("DRILLIP_SERVER", srv.URL+"/tracker")
	output := filepath.Join(t.TempDir(), "backup with spaces.db")
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"backup", "--output", output}, nil, &stdout, &stderr); err != nil {
		t.Fatalf("%v; %s", err, &stderr)
	}
	if stdout.String() != "saved "+output+"\n" {
		t.Fatalf("output=%s", &stdout)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file info=%v err=%v", info, err)
	}
	db, err := sql.Open("sqlite", output)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for table, want := range map[string]int{"errors": 1, "occurrences": 1, "silences": 1} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != want {
			t.Fatalf("%s: %d %v", table, n, err)
		}
	}
	if err := Run(context.Background(), []string{"backup", "--output", output}, nil, &stdout, &stderr); err == nil {
		t.Fatal("existing backup was replaced")
	}
}

func TestBackupCLIFailureDoesNotPublishFiles(t *testing.T) {
	for _, failure := range []string{"unsupported", "server-error", "truncated", "wrong-type", "wrong-header", "redirect", "destination-race"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "backup.db")
			var downloads atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/0/capabilities/" {
					w.Header().Set("Content-Type", "application/json")
					features := `["database_backup"]`
					if failure == "unsupported" {
						features = "[]"
					}
					fmt.Fprintf(w, `{"command_api":1,"features":%s}`, features)
					return
				}
				downloads.Add(1)
				if failure == "server-error" {
					w.WriteHeader(503)
					fmt.Fprint(w, `{"error":"backup busy"}`)
					return
				}
				if failure == "redirect" {
					w.Header().Set("Location", "/elsewhere")
					w.WriteHeader(302)
					return
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				if failure == "wrong-type" {
					w.Header().Set("Content-Type", "text/html")
				}
				w.Header().Set("Content-Length", "100")
				data := append([]byte("SQLite format 3\x00"), bytes.Repeat([]byte{0}, 84)...)
				if failure == "wrong-header" {
					data[0] = 'X'
				}
				if failure == "truncated" {
					data = data[:50]
				}
				if failure == "destination-race" {
					if err := os.WriteFile(output, []byte("keep me"), 0600); err != nil {
						t.Error(err)
					}
				}
				_, _ = w.Write(data)
			}))
			defer srv.Close()
			var stdout, stderr bytes.Buffer
			err := Run(context.Background(), []string{"--server", srv.URL, "backup", "--output", output}, nil, &stdout, &stderr)
			if err == nil || stdout.Len() != 0 {
				t.Fatalf("err=%v output=%s", err, &stdout)
			}
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if failure == "destination-race" {
				data, err := os.ReadFile(output)
				if err != nil || string(data) != "keep me" || len(entries) != 1 {
					t.Fatalf("destination changed: %q %v entries=%v", data, err, entries)
				}
			} else if len(entries) != 0 {
				t.Fatalf("failed backup left files: %v", entries)
			}
			if failure == "unsupported" && (downloads.Load() != 0 || !strings.Contains(err.Error(), "upgrade")) {
				t.Fatalf("unsupported: calls=%d err=%v", downloads.Load(), err)
			}
			if failure == "redirect" && downloads.Load() != 1 {
				t.Fatal("followed redirect")
			}
		})
	}
}

func TestBackupCLIValidatesArgumentsBeforeAccess(t *testing.T) {
	for _, args := range [][]string{{"backup"}, {"backup", "--output", ""}, {"backup", "--output", "file.db", "extra"}} {
		var stdout, stderr bytes.Buffer
		if err := Run(context.Background(), args, nil, &stdout, &stderr); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"backup", "--help"}, nil, &stdout, &stderr); err != nil || !strings.Contains(stdout.String(), "--output") && !strings.Contains(stdout.String(), "-output") {
		t.Fatalf("help: %v %s %s", err, &stdout, &stderr)
	}
}
