package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// Errors must reach main so scripts can use exit status instead of parsing output.
func TestRunCommandErrors(t *testing.T) {
	t.Setenv("DRILLIP_DB", filepath.Join(t.TempDir(), "errors.db"))
	for _, args := range [][]string{
		{"show"}, {"show", "nonsense"}, {"show", "0000000000000000"},
		{"gc", "invalid"}, {"resolve"}, {"--offline", "resolve", "0000000000000000"},
		{"silence"}, {"unsilence"}, {"top", "--unknown"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := Run(context.Background(), args, &stdout, &stderr); err == nil {
				t.Fatal("expected command failure")
			}
			if stdout.Len() != 0 {
				t.Fatalf("failure wrote stdout: %q", stdout.String())
			}
		})
	}
	for _, args := range [][]string{{"top"}, {"top", "--help"}, {"silences"}} {
		var stdout, stderr bytes.Buffer
		if err := Run(context.Background(), args, &stdout, &stderr); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestHealthDeadlineAndWildcardTarget(t *testing.T) {
	t.Setenv("DRILLIP_DB", filepath.Join(t.TempDir(), "missing", "errors.db"))
	for _, stalled := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if stalled {
				<-r.Context().Done()
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
		var stdout, stderr bytes.Buffer
		start := time.Now()
		err := Run(context.Background(), []string{"--addr", "0.0.0.0:" + port, "health"}, &stdout, &stderr)
		srv.Close()
		if stalled {
			if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 4*time.Second {
				t.Fatalf("deadline: %v after %v", err, time.Since(start))
			}
		} else if err != nil || stdout.String() != "ok\n" {
			t.Fatalf("wildcard target: %v, %q", err, stdout.String())
		}
	}
}

func TestServerURL(t *testing.T) {
	for addr, want := range map[string]string{
		":8300": "http://127.0.0.1:8300", "0.0.0.0:8300": "http://127.0.0.1:8300",
		"[::]:8300": "http://[::1]:8300", "example.com:8300": "http://example.com:8300",
	} {
		if got := serverURL(addr); got != want {
			t.Errorf("%s: %s, want %s", addr, got, want)
		}
	}
}
