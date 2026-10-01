package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/adapter/in/api"
	store "github.com/PhilHem/drillip/internal/adapter/out/sqlite"
)

func TestHealthDetailsTracksBackupRestoreAndRestart(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	srv := httptest.NewServer((&api.Handler{Database: s, Backups: s}).Routes())
	defer srv.Close()
	var out, errOut bytes.Buffer
	run := func(target string) string {
		t.Helper()
		out.Reset()
		if err := Run(context.Background(), []string{"--server", target, "health", "--details"}, &out, &errOut); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := run(srv.URL); got != "status: ok\nlast_backup_generated_at: unknown\nlast_restored_at: unknown\nrestored_snapshot_at: unknown\n" {
		t.Fatal(got)
	}
	output := filepath.Join(t.TempDir(), "backup.db")
	if err := Run(context.Background(), []string{"--server", srv.URL, "backup", "--output", output}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if got := run(srv.URL); strings.Contains(got, "last_backup_generated_at: unknown") || !strings.Contains(got, "last_restored_at: unknown") {
		t.Fatal(got)
	}
	restoredPath := filepath.Join(t.TempDir(), "restored.db")
	if err := Run(context.Background(), []string{"restore", "--input", output, "--db", restoredPath}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	restored, err := store.Open(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err = store.Open(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	check := httptest.NewServer(http.StripPrefix("/tracker", (&api.Handler{Database: restored}).Routes()))
	defer check.Close()
	got := run(check.URL + "/tracker")
	if !strings.Contains(got, "last_backup_generated_at: unknown") || strings.Contains(got, "last_restored_at: unknown") || strings.Contains(got, "restored_snapshot_at: unknown") {
		t.Fatal(got)
	}
}

func TestHealthDetailsFailureDoesNotPrintSuccess(t *testing.T) {
	for _, failure := range []string{"unsupported", "invalid-status", "invalid-time", "failed-database", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			operations := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/0/capabilities/" {
					features := []string{"database_history"}
					if failure == "unsupported" {
						features = nil
					}
					json.NewEncoder(w).Encode(map[string]any{"command_api": 1, "features": features})
					return
				}
				operations++
				switch failure {
				case "invalid-status":
					fmt.Fprint(w, `{"status":"maybe"}`)
				case "invalid-time":
					fmt.Fprint(w, `{"status":"ok","last_restored_at":"yesterday"}`)
				case "failed-database":
					w.WriteHeader(503)
					fmt.Fprint(w, `{"error":"database health details failed"}`)
				case "cancelled":
					<-r.Context().Done()
				}
			}))
			defer srv.Close()
			timeout := time.Second
			if failure == "cancelled" {
				timeout = 20 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			var out, errOut bytes.Buffer
			err := Run(ctx, []string{"--server", srv.URL, "health", "--details"}, &out, &errOut)
			if err == nil || out.Len() != 0 {
				t.Fatalf("err=%v output=%s", err, &out)
			}
			if failure == "unsupported" && (operations != 0 || !strings.Contains(err.Error(), "upgrade")) {
				t.Fatal(err)
			}
		})
	}
}
