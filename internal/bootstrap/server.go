package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/PhilHem/drillip/internal/adapter/in/api"
	"github.com/PhilHem/drillip/internal/adapter/in/ingest"
	integrations "github.com/PhilHem/drillip/internal/adapter/out/observability"
	notify "github.com/PhilHem/drillip/internal/adapter/out/smtp"
	store "github.com/PhilHem/drillip/internal/adapter/out/sqlite"
	outport "github.com/PhilHem/drillip/internal/application/port/out"
	"github.com/PhilHem/drillip/internal/application/service"
)

func runServe(ctx context.Context, cfg config) (err error) {
	s, err := store.Open(cfg.DB)
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}

	defer func() {
		if checkpointErr := s.Checkpoint(); checkpointErr != nil {
			err = errors.Join(err, fmt.Errorf("checkpoint db: %w", checkpointErr))
		}
		err = errors.Join(err, s.Close())
	}()

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	var notifier outport.Notifier
	if cfg.SMTP.Enabled() {
		mailer := notify.NewNotifier(cfg.SMTP, cfg.Project, cfg.SMTPCooldown, cfg.SMTPDigest, func(fp string) {
			if err := s.MarkNotified(fp); err != nil {
				slog.Error("notify: failed to mark notified", "fingerprint", fp, "err", err)
			}
		})
		notifier = mailer
		defer mailer.Close()
		slog.Info("email notifications enabled", "to", cfg.SMTP.To, "via", cfg.SMTP.Addr(), "cooldown", cfg.SMTPCooldown, "digest", cfg.SMTPDigest, "skip_verify", cfg.SMTP.SkipVerify)
	}

	app := service.New(s, notifier, integrations.Client{Config: cfg.Integrations})
	apiHandler := &api.Handler{Errors: app, Correlation: app, Notifications: app}
	healthHandler := ingest.HandleHealth(app)

	mux := http.NewServeMux()
	mux.HandleFunc("/", healthHandler)
	mux.HandleFunc("/api/", ingest.MakeHandler(app))
	mux.Handle("/api/0/", apiHandler.Routes())
	mux.HandleFunc("/-/healthy", healthHandler)

	srv := &http.Server{Addr: cfg.Addr, Handler: mux}

	// Background maintenance goroutine
	maint := &service.Maintenance{Store: s, Notifier: notifier, ResolveAfter: cfg.ResolveAfter, RetainFor: cfg.RetainFor}
	maintCtx, cancelMaint := context.WithCancel(ctx)
	maintDone := make(chan struct{})
	go func() {
		defer close(maintDone)
		maint.Run(maintCtx)
	}()
	defer func() {
		cancelMaint()
		<-maintDone
	}()

	slog.Info("drillip listening", "addr", listener.Addr(), "db", cfg.DB)
	return serveHTTP(ctx, srv, listener)
}

// serveHTTP waits for active requests to finish before returning on cancellation.
func serveHTTP(ctx context.Context, srv *http.Server, listener net.Listener) error {
	serveDone := make(chan struct{})
	shutdownDone := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
		case <-serveDone:
		}
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := srv.Shutdown(shutdownCtx)
		if err != nil {
			// Release active connections if the grace period expires.
			err = errors.Join(err, srv.Close())
		}
		shutdownDone <- err
	}()

	err := srv.Serve(listener)
	close(serveDone)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, <-shutdownDone)
}
