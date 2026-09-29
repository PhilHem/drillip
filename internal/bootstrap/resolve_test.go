package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/adapter/in/api"
	store "github.com/PhilHem/drillip/internal/adapter/out/sqlite"
	"github.com/PhilHem/drillip/internal/application/service"
	"github.com/PhilHem/drillip/internal/domain"
)

type resolutionRecorder struct{ received chan []domain.ResolvedError }

func (n *resolutionRecorder) NotifyResolved(events []domain.ResolvedError) { n.received <- events }
func (*resolutionRecorder) NotifyNewError(*domain.Event, string, bool, time.Duration) {
	panic("unexpected ingestion notification")
}
func (*resolutionRecorder) SendTestEmail() error { panic("unexpected test email") }
func (*resolutionRecorder) Recipient() string    { return "local-test" }

func TestResolveCLIUsesServerPolicyAndNeverOpensLocalDatabase(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	notifier := &resolutionRecorder{received: make(chan []domain.ResolvedError, 2)}
	app := service.New(s, notifier, nil)
	h := &api.Handler{Errors: app}
	srv := httptest.NewServer(http.HandlerFunc(h.HandleResolve))
	defer srv.Close()
	t.Setenv("DRILLIP_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("DRILLIP_DB", filepath.Join(t.TempDir(), "missing", "client.db"))
	for _, viaCLI := range []bool{true, false} {
		result, err := s.StoreEvent(&domain.Event{Message: fmt.Sprint("event via CLI: ", viaCLI)})
		if err != nil {
			t.Fatal(err)
		}
		if viaCLI {
			var stdout, stderr bytes.Buffer
			if err := Run(context.Background(), []string{"resolve", result.Fingerprint[:8]}, &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != "resolved "+result.Fingerprint+"\n" {
				t.Fatalf("output: %s", &stdout)
			}
		} else {
			resp, err := http.Post(srv.URL+"/api/0/resolve/"+result.Fingerprint+"/", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("HTTP status %d", resp.StatusCode)
			}
		}
		select {
		case events := <-notifier.received:
			if len(events) != 1 || events[0].Fingerprint != result.Fingerprint {
				t.Fatalf("notification: %+v", events)
			}
		case <-time.After(time.Second):
			t.Fatal("missing resolution notification")
		}
	}
}

func TestResolveOfflineIsExplicitAndNoFallbackOccurs(t *testing.T) {
	db := filepath.Join(t.TempDir(), "errors.db")
	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	event, err := s.StoreEvent(&domain.Event{Message: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	var serverCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "temporarily unavailable"})
	}))
	defer srv.Close()
	t.Setenv("DRILLIP_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("DRILLIP_DB", db)
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"resolve", event.Fingerprint}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("server failure: %v", err)
	}
	detail, err := s.GetDetail(event.Fingerprint)
	if err != nil || detail.ResolvedAt != "" {
		t.Fatal("failed HTTP request mutated local database")
	}
	if err := Run(context.Background(), []string{"--offline", "resolve", event.Fingerprint}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	detail, _ = s.GetDetail(event.Fingerprint)
	if detail.ResolvedAt == "" || serverCalls.Load() != 1 {
		t.Fatalf("offline result: %+v, calls=%d", detail, serverCalls.Load())
	}
	for _, args := range [][]string{{"--offline", "top"}, {"--db", db, "resolve", event.Fingerprint}} {
		if err := Run(context.Background(), args, &stdout, &stderr); err == nil {
			t.Fatalf("accepted invalid mode: %v", args)
		}
	}
}

func TestResolveRequestHonorsCancellationAndReportsErrors(t *testing.T) {
	for _, status := range []int{404, 409, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		_, err := resolveOnServer(context.Background(), strings.TrimPrefix(srv.URL, "http://"), "abcd")
		srv.Close()
		if err == nil {
			t.Fatalf("ignored HTTP %d", status)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := resolveOnServer(ctx, strings.TrimPrefix(srv.URL, "http://"), "abcd")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
}
