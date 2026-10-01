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
	"github.com/PhilHem/drillip/internal/adapter/out/httpclient"
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
	srv := httptest.NewServer(h.Routes())
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
			if err := Run(context.Background(), []string{"resolve", result.Fingerprint[:8]}, nil, &stdout, &stderr); err != nil {
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

func TestResolveFailureNeverFallsBackToLocalDatabase(t *testing.T) {
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
		if r.URL.Path == "/api/0/capabilities/" {
			fmt.Fprint(w, `{"command_api":1}`)
			return
		}
		serverCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "temporarily unavailable"})
	}))
	defer srv.Close()
	t.Setenv("DRILLIP_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("DRILLIP_DB", db)
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"resolve", event.Fingerprint}, nil, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("server failure: %v", err)
	}
	detail, err := s.GetDetail(event.Fingerprint)
	if err != nil || detail.ResolvedAt != "" {
		t.Fatal("failed HTTP request mutated local database")
	}
	for _, args := range [][]string{{"--offline", "top"}, {"--db", db, "resolve", event.Fingerprint}, {"maintenance", "--db", db, "resolve", event.Fingerprint}} {
		if err := Run(context.Background(), args, nil, &stdout, &stderr); err == nil {
			t.Fatalf("accepted invalid mode: %v", args)
		}
	}
	detail, err = s.GetDetail(event.Fingerprint)
	if err != nil || detail.ResolvedAt != "" || serverCalls.Load() != 1 {
		t.Fatalf("failed commands changed state or retried: %+v, calls=%d, err=%v", detail, serverCalls.Load(), err)
	}
}

func TestResolveRequestHonorsCancellationAndReportsErrors(t *testing.T) {
	for _, status := range []int{404, 409, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		client, _ := httpclient.New(srv.URL)
		_, err := client.Resolve(context.Background(), "abcd")
		srv.Close()
		if err == nil {
			t.Fatalf("ignored HTTP %d", status)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	client, _ := httpclient.New(srv.URL)
	_, err := client.Resolve(ctx, "abcd")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
}
