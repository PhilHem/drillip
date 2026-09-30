// Package bootstrap wires the internal packages and runs Drillip commands.
package bootstrap

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/PhilHem/drillip/internal/adapter/in/cli"
	"github.com/PhilHem/drillip/internal/adapter/out/httpclient"
	store "github.com/PhilHem/drillip/internal/adapter/out/sqlite"
	"github.com/PhilHem/drillip/internal/application/service"
)

// Run parses an invocation before connecting its explicitly selected backend.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	initLogger(stderr)
	err := run(ctx, args, stdout, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}
func flags(name string, w io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { fmt.Fprintf(w, "Usage of %s:\n", name); fs.SetOutput(w); fs.PrintDefaults() }
	return fs
}
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flags("drillip", stderr)
	server := fs.String("server", "", "Drillip server URL (overrides DRILLIP_SERVER)")
	legacyAddr := fs.String("addr", "", "deprecated: use --server URL or serve --listen address")
	legacyDB := fs.String("db", "", "deprecated global server DB option; use serve --db or maintenance --db")
	offline := fs.Bool("offline", false, "removed: use maintenance --db PATH COMMAND")
	globalUsage := fs.Usage
	fs.Usage = func() {
		globalUsage()
		fmt.Fprint(stderr, `
Commands:
  list         Browse and search all error groups with pagination
  top          Rank error groups by total occurrence count
  recent       Show error groups first seen within a lookback window
  show         Inspect an error group
  trend        Show an error group's hourly occurrences
  releases     Show an error group's occurrences by release
  correlate    Inspect telemetry around an error occurrence
  stats        Show error and occurrence totals
  resolve      Mark an error group resolved
  silence      Silence notifications for an error group
  silences     List active notification silences
  unsilence    Remove an error group's notification silence
  gc           Delete occurrences older than a duration
  health       Check server health
  serve        Run the server (default command)
  maintenance  Run a command against an existing local database

Use drillip COMMAND --help for command options.
`)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	var emptyOption string
	fs.Visit(func(f *flag.Flag) {
		if f.Name != "offline" && f.Value.String() == "" {
			emptyOption = f.Name
		}
	})
	if emptyOption != "" {
		return fmt.Errorf("--%s requires a nonempty value", emptyOption)
	}
	if *offline {
		return fmt.Errorf("--offline was replaced by maintenance --db PATH COMMAND")
	}
	rest := fs.Args()
	mode := "serve"
	if len(rest) > 0 {
		mode = rest[0]
		rest = rest[1:]
	}
	if *server != "" && *legacyAddr != "" {
		return fmt.Errorf("use --server or legacy --addr, not both")
	}
	switch mode {
	case "serve":
		if *server != "" {
			return fmt.Errorf("--server selects a client target; use serve --listen")
		}
		cfg := loadConfig()
		if *legacyAddr != "" {
			cfg.Addr = *legacyAddr
		}
		if *legacyDB != "" {
			cfg.DB = *legacyDB
		}
		serve := flags("drillip serve", stderr)
		serve.StringVar(&cfg.Addr, "listen", cfg.Addr, "listen address")
		serve.StringVar(&cfg.DB, "db", cfg.DB, "SQLite database path")
		if err := serve.Parse(rest); err != nil {
			return err
		}
		if serve.NArg() != 0 {
			return fmt.Errorf("unexpected serve arguments")
		}
		if cfg.DB == "" || cfg.Addr == "" {
			return fmt.Errorf("serve requires a nonempty database path and listen address")
		}
		validateConfig(cfg)
		return runServe(ctx, cfg)
	case "maintenance":
		if *server != "" || *legacyAddr != "" || *legacyDB != "" {
			return fmt.Errorf("maintenance uses maintenance --db PATH; server/global database options are not accepted")
		}
		local := flags("drillip maintenance", stderr)
		db := local.String("db", "", "required existing SQLite database path; no network or notifications")
		if err := local.Parse(rest); err != nil {
			return err
		}
		cmd, err := cli.Parse(local.Args(), stdout)
		if err != nil {
			return err
		}
		if local.Arg(0) == "health" {
			return fmt.Errorf("health checks a server and is unavailable in maintenance")
		}
		if *db == "" {
			return fmt.Errorf("maintenance requires an explicit --db PATH")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Stat(*db)
		if err != nil {
			return fmt.Errorf("maintenance database: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("maintenance database must be an existing file")
		}
		s, err := store.Open(*db)
		if err != nil {
			return fmt.Errorf("init db: %w", err)
		}
		defer s.Close()
		app := service.New(s, nil, nil)
		return cmd.Run(ctx, &cli.CLI{Errors: app, Correlation: app,
			CommandPrefix: []string{"drillip", "maintenance", "--db", *db}}, stdout)
	default:
		cmd, err := cli.Parse(append([]string{mode}, rest...), stdout)
		if err != nil {
			return err
		}
		if *legacyDB != "" {
			return fmt.Errorf("normal commands use the server; use maintenance --db PATH COMMAND for local access")
		}
		target := *server
		if target == "" {
			target = os.Getenv("DRILLIP_SERVER")
		}
		if *legacyAddr != "" {
			target = serverURL(*legacyAddr)
		} else if target == "" && os.Getenv("DRILLIP_ADDR") != "" {
			target = serverURL(os.Getenv("DRILLIP_ADDR"))
		}
		if target == "" {
			target = "http://127.0.0.1:8300"
		}
		client, err := httpclient.New(target)
		if err != nil {
			return err
		}
		if mode == "health" {
			if err := client.Health(ctx); err != nil {
				return fmt.Errorf("unhealthy: %w", err)
			}
			_, err = fmt.Fprintln(stdout, "ok")
			return err
		}
		return cmd.Run(ctx, &cli.CLI{Errors: client, Correlation: client,
			CommandPrefix: []string{"drillip", "--server", target}}, stdout)
	}
}

// serverURL preserves legacy listen-address targeting, including wildcard loopback.
func serverURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err == nil {
		if host == "" || host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		if host == "::" {
			host = "::1"
		}
		addr = net.JoinHostPort(host, port)
	}
	return "http://" + addr
}
