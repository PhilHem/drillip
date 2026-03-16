package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PhilHem/drillip/api"
	"github.com/PhilHem/drillip/cli"
	"github.com/PhilHem/drillip/domain"
	"github.com/PhilHem/drillip/ingest"
	"github.com/PhilHem/drillip/integrations"
	"github.com/PhilHem/drillip/notify"
	"github.com/PhilHem/drillip/store"
)

// Config holds all environment-based configuration.
type Config struct {
	DB           string
	Addr         string
	Project      string // project name for notifications
	SMTP         notify.SMTPConfig
	SMTPCooldown time.Duration
	SMTPDigest   time.Duration
	ResolveAfter time.Duration
	RetainFor    time.Duration
	Integrations integrations.Config
}

func loadConfig() Config {
	cfg := Config{
		DB:   "errors.db",
		Addr: "127.0.0.1:8300",
	}
	if v := os.Getenv("DRILLIP_DB"); v != "" {
		cfg.DB = v
	}
	if v := os.Getenv("DRILLIP_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("DRILLIP_PROJECT"); v != "" {
		cfg.Project = v
	}
	if v := os.Getenv("DRILLIP_UNIT"); v != "" {
		cfg.Integrations.Unit = v
	}
	if v := os.Getenv("DRILLIP_VM_URL"); v != "" {
		cfg.Integrations.VMURL = v
	}
	if v := os.Getenv("DRILLIP_VT_URL"); v != "" {
		cfg.Integrations.VTURL = v
	}
	if v := os.Getenv("DRILLIP_PYROSCOPE_URL"); v != "" {
		cfg.Integrations.PyroscopeURL = v
	}
	if v := os.Getenv("DRILLIP_SERVICE"); v != "" {
		cfg.Integrations.Service = v
	}
	if v := os.Getenv("DRILLIP_SMTP_HOST"); v != "" {
		cfg.SMTP.Host = v
	}
	if v := os.Getenv("DRILLIP_SMTP_PORT"); v != "" {
		cfg.SMTP.Port = v
	}
	if v := os.Getenv("DRILLIP_SMTP_FROM"); v != "" {
		cfg.SMTP.From = v
	}
	if v := os.Getenv("DRILLIP_SMTP_TO"); v != "" {
		cfg.SMTP.To = v
	}
	if v := os.Getenv("DRILLIP_SMTP_USER"); v != "" {
		cfg.SMTP.User = v
	}
	if v := os.Getenv("DRILLIP_SMTP_PASS"); v != "" {
		cfg.SMTP.Pass = v
	}
	cfg.SMTPCooldown = 60 * time.Second // default
	if v := os.Getenv("DRILLIP_SMTP_COOLDOWN"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.SMTPCooldown = d
		} else {
			slog.Warn("invalid DRILLIP_SMTP_COOLDOWN, using default", "value", v, "default", cfg.SMTPCooldown)
		}
	}
	cfg.SMTPDigest = 5 * time.Minute // default
	if v := os.Getenv("DRILLIP_SMTP_DIGEST"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.SMTPDigest = d
		} else {
			slog.Warn("invalid DRILLIP_SMTP_DIGEST, using default", "value", v, "default", cfg.SMTPDigest)
		}
	}
	cfg.ResolveAfter = 24 * time.Hour // default
	if v := os.Getenv("DRILLIP_RESOLVE_AFTER"); v != "" {
		if d, err := domain.ParseDuration(v); err == nil {
			cfg.ResolveAfter = d
		} else {
			slog.Warn("invalid DRILLIP_RESOLVE_AFTER, using default", "value", v, "default", cfg.ResolveAfter)
		}
	}
	cfg.RetainFor = 90 * 24 * time.Hour // default 90 days
	if v := os.Getenv("DRILLIP_RETAIN"); v != "" {
		if d, err := domain.ParseDuration(v); err == nil {
			cfg.RetainFor = d
		} else {
			slog.Warn("invalid DRILLIP_RETAIN, using default", "value", v, "default", cfg.RetainFor)
		}
	}
	return cfg
}

func validateConfig(cfg Config) {
	if cfg.SMTP.Host != "" && cfg.SMTP.To == "" {
		slog.Warn("DRILLIP_SMTP_HOST set but DRILLIP_SMTP_TO empty, notifications disabled")
	}
	if cfg.SMTP.Host != "" && cfg.SMTP.From == "" {
		slog.Warn("DRILLIP_SMTP_HOST set but DRILLIP_SMTP_FROM empty")
	}
	slog.Info("config loaded", "db", cfg.DB, "addr", cfg.Addr, "resolve_after", cfg.ResolveAfter, "cooldown", cfg.SMTPCooldown, "digest", cfg.SMTPDigest)
}

func runHealthCmd(cfg Config) {
	resp, err := http.Get("http://" + cfg.Addr + "/-/healthy")
	if err != nil {
		fmt.Fprintf(os.Stderr, "unhealthy: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "unhealthy: status %d\n", resp.StatusCode)
		os.Exit(1)
	}
	fmt.Println("ok")
}

func runServe(cfg Config) {
	s, err := store.Open(cfg.DB)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}

	var notifier *notify.Notifier
	if cfg.SMTP.Enabled() {
		notifier = notify.NewNotifier(cfg.SMTP, cfg.Project, cfg.SMTPCooldown, cfg.SMTPDigest, s)
		slog.Info("email notifications enabled", "to", cfg.SMTP.To, "via", cfg.SMTP.Addr(), "cooldown", cfg.SMTPCooldown, "digest", cfg.SMTPDigest)
	}

	apiHandler := &api.Handler{DB: s.DB, Store: s}
	healthHandler := ingest.HandleHealth(s.DB)

	mux := http.NewServeMux()
	mux.HandleFunc("/", healthHandler)
	mux.HandleFunc("/api/", ingest.MakeHandler(s, notifier))
	mux.HandleFunc("/-/healthy", healthHandler)
	mux.HandleFunc("/api/0/top/", apiHandler.HandleTop)
	mux.HandleFunc("/api/0/recent/", apiHandler.HandleRecent)
	mux.HandleFunc("/api/0/show/", apiHandler.HandleShow)
	mux.HandleFunc("/api/0/trend/", apiHandler.HandleTrend)
	mux.HandleFunc("/api/0/releases/", apiHandler.HandleReleases)
	mux.HandleFunc("/api/0/stats/", apiHandler.HandleStats)
	mux.HandleFunc("/api/0/gc/", apiHandler.HandleGC)
	mux.HandleFunc("/api/0/resolve/", apiHandler.HandleResolve)
	mux.HandleFunc("/api/0/silence/", apiHandler.HandleSilence)
	mux.HandleFunc("/api/0/silences/", apiHandler.HandleListSilences)

	srv := &http.Server{Addr: cfg.Addr, Handler: mux}

	// Background auto-resolve goroutine
	stopResolve := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n, err := s.AutoResolve(cfg.ResolveAfter)
				if err != nil {
					slog.Error("auto-resolve error", "err", err)
				} else if n > 0 {
					slog.Info("auto-resolved errors", "count", n, "older_than", cfg.ResolveAfter)
				}
				if pruned, err := s.PruneExpiredSilences(); err != nil {
					slog.Error("prune silences error", "err", err)
				} else if pruned > 0 {
					slog.Info("pruned expired silences", "count", pruned)
				}
				if cfg.RetainFor > 0 {
					threshold := time.Now().UTC().Add(-cfg.RetainFor)
					deleted, err := s.GCOccurrences(threshold)
					if err != nil {
						slog.Error("gc occurrences failed", "error", err)
					} else if deleted > 0 {
						slog.Info("gc: pruned old occurrences", "deleted", deleted, "older_than", cfg.RetainFor)
					}
				}
			case <-stopResolve:
				return
			}
		}
	}()

	// Graceful shutdown: checkpoint WAL and close DB
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-stop
		signal.Stop(stop)
		slog.Info("shutting down")
		close(stopResolve)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	slog.Info("drillip listening", "addr", cfg.Addr, "db", cfg.DB)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}

	if notifier != nil {
		notifier.Close()
	}
	_ = s.Checkpoint()
	slog.Info("WAL checkpoint complete")
	s.Close()
}

func main() {
	cfg := loadConfig()

	// Parse global flags before subcommand
	globalFlags := flag.NewFlagSet("drillip", flag.ContinueOnError)
	dbFlag := globalFlags.String("db", "", "database path (overrides DRILLIP_DB)")
	addrFlag := globalFlags.String("addr", "", "listen address (overrides DRILLIP_ADDR)")
	_ = globalFlags.Parse(os.Args[1:])

	if *dbFlag != "" {
		cfg.DB = *dbFlag
	}
	if *addrFlag != "" {
		cfg.Addr = *addrFlag
	}

	validateConfig(cfg)

	remaining := globalFlags.Args()

	// No args or "serve" -> start HTTP server
	if len(remaining) == 0 || remaining[0] == "serve" {
		runServe(cfg)
		return
	}

	// CLI commands need the DB
	s, err := store.Open(cfg.DB)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer s.Close()

	c := &cli.CLI{DB: s.DB, Store: s}
	cmd := remaining[0]
	args := remaining[1:]

	switch cmd {
	case "top":
		c.RunTop(args, os.Stdout)
	case "recent":
		c.RunRecent(args, os.Stdout)
	case "show":
		c.RunShow(args, os.Stdout)
	case "trend":
		c.RunTrend(args, os.Stdout)
	case "correlate":
		c.RunCorrelate(args, os.Stdout, cfg.Integrations)
	case "releases":
		c.RunReleases(args, os.Stdout)
	case "stats":
		c.RunStats(args, os.Stdout)
	case "gc":
		c.RunGC(args, os.Stdout)
	case "resolve":
		c.RunResolve(args, os.Stdout)
	case "silence":
		c.RunSilence(args, os.Stdout)
	case "silences":
		c.RunSilences(args, os.Stdout)
	case "unsilence":
		c.RunUnsilence(args, os.Stdout)
	case "health":
		runHealthCmd(cfg)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		fmt.Fprintln(os.Stderr, "commands: serve, top, recent, show, trend, correlate, releases, stats, gc, resolve, silence, silences, unsilence, health")
		os.Exit(1)
	}
}
