package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGlobalFlags(t *testing.T) {
	// Neither help nor invalid flags should start a server or open the database.
	t.Setenv("DRILLIP_DB", filepath.Join(t.TempDir(), "missing", "errors.db"))
	for _, tc := range []struct {
		arg     string
		wantErr string
	}{
		{"--help", ""},
		{"--unknown", "flag provided but not defined"},
		{"--db", "flag needs an argument"},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := Run(context.Background(), []string{tc.arg}, &stdout, &stderr)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(stderr.String(), "Usage of drillip:") {
					t.Fatalf("missing help output: %q", stderr.String())
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestRunHealthDoesNotOpenDatabase(t *testing.T) {
	t.Setenv("DRILLIP_DB", filepath.Join(t.TempDir(), "missing", "errors.db"))
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/-/healthy" {
				t.Errorf("unexpected health path: %s", r.URL.Path)
			}
			w.WriteHeader(status)
		}))
		var stdout, stderr bytes.Buffer
		err := Run(context.Background(), []string{"--addr", strings.TrimPrefix(srv.URL, "http://"), "health"}, &stdout, &stderr)
		srv.Close()
		if status == http.StatusOK {
			if err != nil || stdout.String() != "ok\n" {
				t.Fatalf("healthy result: output %q, error %v", stdout.String(), err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "unhealthy: status 503") {
			t.Fatalf("unhealthy result: %v", err)
		}
	}
}

func TestRunUnknownCommand(t *testing.T) {
	t.Setenv("DRILLIP_DB", filepath.Join(t.TempDir(), "errors.db"))
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"unknown"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unknown command: unknown") {
		t.Fatalf("error = %v, want unknown command", err)
	}
}
