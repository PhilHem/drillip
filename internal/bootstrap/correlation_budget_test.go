package bootstrap

import (
	"bytes"
	"context"
	"github.com/PhilHem/drillip/internal/adapter/in/api"
	integrations "github.com/PhilHem/drillip/internal/adapter/out/observability"
	store "github.com/PhilHem/drillip/internal/adapter/out/sqlite"
	"github.com/PhilHem/drillip/internal/application/service"
	"github.com/PhilHem/drillip/internal/domain"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlowOptionalMetricsCannotEraseStoredCorrelation(t *testing.T) {
	telemetry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer telemetry.Close()
	s, err := store.Open(filepath.Join(t.TempDir(), "errors.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	event, err := s.StoreEvent(&domain.Event{Message: "stored context survives"})
	if err != nil {
		t.Fatal(err)
	}
	app := service.New(s, nil, integrations.Client{Config: integrations.Config{VMURL: telemetry.URL}})
	h := &api.Handler{Errors: app, Correlation: app}
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()
	var out, errout bytes.Buffer
	start := time.Now()
	err = Run(context.Background(), []string{"--server", srv.URL, "correlate", event.Fingerprint}, &out, &errout)
	if err != nil || !strings.Contains(out.String(), event.Fingerprint) || !strings.Contains(out.String(), "stored context survives") || time.Since(start) > 8*time.Second {
		t.Fatalf("err=%v elapsed=%v output=%s", err, time.Since(start), &out)
	}
}
