// Package bootstrap wires the internal packages and runs Drillip commands.
package bootstrap

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/PhilHem/drillip/internal/adapter/in/cli"
	integrations "github.com/PhilHem/drillip/internal/adapter/out/observability"
	store "github.com/PhilHem/drillip/internal/adapter/out/sqlite"
	"github.com/PhilHem/drillip/internal/application/service"
	"github.com/PhilHem/drillip/internal/domain"
)

// Run configures logging and executes the server or a CLI command.
// The caller owns signal handling and cancels ctx to stop the application.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	initLogger(stderr)
	cfg := loadConfig()

	// Parse global flags before subcommand
	globalFlags := flag.NewFlagSet("drillip", flag.ContinueOnError)
	globalFlags.SetOutput(io.Discard)
	dbFlag := globalFlags.String("db", "", "database path (overrides DRILLIP_DB)")
	offlineFlag := globalFlags.Bool("offline", false, "resolve directly in SQLite without notifications")
	addrFlag := globalFlags.String("addr", "", "listen address (overrides DRILLIP_ADDR)")
	if err := globalFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			globalFlags.SetOutput(stderr)
			globalFlags.Usage()
			return nil
		}
		return err
	}

	if *dbFlag != "" {
		cfg.DB = *dbFlag
	}
	if *addrFlag != "" {
		cfg.Addr = *addrFlag
	}

	validateConfig(cfg)

	remaining := globalFlags.Args()
	if *offlineFlag && (len(remaining) == 0 || remaining[0] != "resolve") {
		return fmt.Errorf("--offline is only supported with resolve")
	}
	if len(remaining) > 0 && remaining[0] == "resolve" && !*offlineFlag {
		if *dbFlag != "" {
			return fmt.Errorf("resolve uses the server; --db requires --offline")
		}
		return cli.RunResolve(remaining[1:], stdout, func(fp string) (domain.ResolveResult, error) {
			return resolveOnServer(ctx, cfg.Addr, fp)
		})
	}

	// No args or "serve" -> start HTTP server
	if len(remaining) == 0 || remaining[0] == "serve" {
		return runServe(ctx, cfg)
	}

	if remaining[0] == "health" {
		if len(remaining) != 1 {
			return fmt.Errorf("usage: drillip health")
		}
		return runHealthCmd(ctx, cfg, stdout)
	}

	// Investigation commands need the DB
	s, err := store.Open(cfg.DB)
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}
	defer s.Close()

	app := service.New(s, nil, integrations.Client{Config: cfg.Integrations})
	c := &cli.CLI{Errors: app, Correlation: app}
	cmd := remaining[0]
	args = remaining[1:]

	switch cmd {
	case "top":
		err = c.RunTop(args, stdout)
	case "recent":
		err = c.RunRecent(args, stdout)
	case "show":
		err = c.RunShow(args, stdout)
	case "trend":
		err = c.RunTrend(args, stdout)
	case "correlate":
		err = c.RunCorrelate(args, stdout)
	case "releases":
		err = c.RunReleases(args, stdout)
	case "stats":
		err = c.RunStats(args, stdout)
	case "gc":
		err = c.RunGC(args, stdout)
	case "resolve":
		err = cli.RunResolve(args, stdout, app.Resolve)
	case "silence":
		err = c.RunSilence(args, stdout)
	case "silences":
		err = c.RunSilences(args, stdout)
	case "unsilence":
		err = c.RunUnsilence(args, stdout)
	default:
		return fmt.Errorf("unknown command: %s\ncommands: serve, top, recent, show, trend, correlate, releases, stats, gc, resolve, silence, silences, unsilence, health", cmd)
	}
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func runHealthCmd(ctx context.Context, cfg config, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL(cfg.Addr)+"/-/healthy", nil)
	if err != nil {
		return fmt.Errorf("unhealthy: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unhealthy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: status %d", resp.StatusCode)
	}
	_, err = fmt.Fprintln(stdout, "ok")
	return err
}

// serverURL turns a listen address into a connectable HTTP target.
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
