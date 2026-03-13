package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// Config holds all environment-based configuration.
type Config struct {
	DB           string
	Addr         string
	Unit         string // journalctl unit name
	VMURL        string // VictoriaMetrics base URL
	VTURL        string // VictoriaTraces base URL
	PyroscopeURL string
	Service      string // service name for Pyroscope
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
	if v := os.Getenv("DRILLIP_UNIT"); v != "" {
		cfg.Unit = v
	}
	if v := os.Getenv("DRILLIP_VM_URL"); v != "" {
		cfg.VMURL = v
	}
	if v := os.Getenv("DRILLIP_VT_URL"); v != "" {
		cfg.VTURL = v
	}
	if v := os.Getenv("DRILLIP_PYROSCOPE_URL"); v != "" {
		cfg.PyroscopeURL = v
	}
	if v := os.Getenv("DRILLIP_SERVICE"); v != "" {
		cfg.Service = v
	}
	return cfg
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
	if err := initDB(cfg.DB); err != nil {
		log.Fatalf("init db: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleHealth)
	mux.HandleFunc("/api/", handleIngest)
	mux.HandleFunc("/-/healthy", handleHealth)
	mux.HandleFunc("/api/0/top/", handleAPITop)
	mux.HandleFunc("/api/0/show/", handleAPIShow)
	mux.HandleFunc("/api/0/stats/", handleAPIStats)

	srv := &http.Server{Addr: cfg.Addr, Handler: mux}

	// Graceful shutdown: checkpoint WAL and close DB
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-stop
		log.Println("shutting down...")
		_ = srv.Shutdown(context.Background())
	}()

	log.Printf("drillip listening on %s (db: %s)", cfg.Addr, cfg.DB)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}

	_, _ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	log.Println("WAL checkpoint complete")
	db.Close()
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

	remaining := globalFlags.Args()

	// No args or "serve" → start HTTP server
	if len(remaining) == 0 || remaining[0] == "serve" {
		runServe(cfg)
		return
	}

	// CLI commands need the DB
	if err := initDB(cfg.DB); err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	cmd := remaining[0]
	args := remaining[1:]

	switch cmd {
	case "top":
		runTop(args, os.Stdout)
	case "recent":
		runRecent(args, os.Stdout)
	case "show":
		runShow(args, os.Stdout)
	case "trend":
		runTrend(args, os.Stdout)
	case "correlate":
		runCorrelate(args, os.Stdout, cfg)
	case "releases":
		runReleases(args, os.Stdout)
	case "stats":
		runStats(args, os.Stdout)
	case "gc":
		runGC(args, os.Stdout)
	case "health":
		runHealthCmd(cfg)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		fmt.Fprintln(os.Stderr, "commands: serve, top, recent, show, trend, correlate, releases, stats, gc, health")
		os.Exit(1)
	}
}
