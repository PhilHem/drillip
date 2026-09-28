// Package app wires the internal packages and runs Drillip commands.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"

	"github.com/PhilHem/drillip/internal/cli"
	"github.com/PhilHem/drillip/internal/store"
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

	// No args or "serve" -> start HTTP server
	if len(remaining) == 0 || remaining[0] == "serve" {
		return runServe(ctx, cfg)
	}

	if remaining[0] == "health" {
		return runHealthCmd(ctx, cfg, stdout)
	}

	// Investigation commands need the DB
	s, err := store.Open(cfg.DB)
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}
	defer s.Close()

	c := &cli.CLI{Store: s, Integrations: cfg.Integrations}
	cmd := remaining[0]
	args = remaining[1:]

	switch cmd {
	case "top":
		c.RunTop(args, stdout)
	case "recent":
		c.RunRecent(args, stdout)
	case "show":
		c.RunShow(args, stdout)
	case "trend":
		c.RunTrend(args, stdout)
	case "correlate":
		c.RunCorrelate(args, stdout)
	case "releases":
		c.RunReleases(args, stdout)
	case "stats":
		c.RunStats(args, stdout)
	case "gc":
		c.RunGC(args, stdout)
	case "resolve":
		c.RunResolve(args, stdout)
	case "silence":
		c.RunSilence(args, stdout)
	case "silences":
		c.RunSilences(args, stdout)
	case "unsilence":
		c.RunUnsilence(args, stdout)
	default:
		return fmt.Errorf("unknown command: %s\ncommands: serve, top, recent, show, trend, correlate, releases, stats, gc, resolve, silence, silences, unsilence, health", cmd)
	}
	return nil
}

func runHealthCmd(ctx context.Context, cfg config, stdout io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+cfg.Addr+"/-/healthy", nil)
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
