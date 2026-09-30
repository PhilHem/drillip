package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// Command is a validated invocation. Parse performs no data or network access;
// callers connect the selected backend only after parsing succeeds.
type Command struct {
	name, reference, reason, durationText string
	limit, hours, nth                     int
	filter                                domain.ListFilter
	listQuery                             domain.ListQuery
	output                                string
	duration                              time.Duration
}

func Parse(args []string, help io.Writer) (*Command, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("missing command")
	}
	cmd := &Command{name: args[0]}
	var usage bytes.Buffer
	fs := flag.NewFlagSet("drillip "+cmd.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		fmt.Fprintf(&usage, "Usage: drillip %s [options]", cmd.name)
		switch cmd.name {
		case "show", "trend", "releases", "correlate", "resolve", "unsilence":
			fmt.Fprint(&usage, " <fingerprint>")
		case "silence":
			fmt.Fprint(&usage, " <fingerprint> [duration]")
		case "gc":
			fmt.Fprint(&usage, " <duration>")
		}
		fmt.Fprintln(&usage)
		switch cmd.name {
		case "list":
			fmt.Fprintln(&usage, "Browse resolved and unresolved error groups, newest last-seen first by default.")
		case "top":
			fmt.Fprintln(&usage, "Show error groups ranked by total occurrence count.")
		case "recent":
			fmt.Fprintln(&usage, "Show error groups first seen within the lookback window.")
		}
		fs.SetOutput(&usage)
		fs.PrintDefaults()
	}
	var tag string
	switch cmd.name {
	case "backup":
		fs.StringVar(&cmd.output, "output", "", "required new file for the database backup")
	case "list":
		fs.StringVar(&cmd.listQuery.Search, "search", "", "literal substring in full stored type or message (ASCII case-insensitive)")
		fs.StringVar(&cmd.listQuery.Sort, "sort", domain.SortLastSeen, "order by last_seen or count, descending")
		fs.IntVar(&cmd.listQuery.Limit, "limit", domain.DefaultListLimit, fmt.Sprintf("number of errors to show (1–%d)", domain.MaxListLimit))
		fs.IntVar(&cmd.listQuery.Offset, "offset", 0, "number of matching errors to skip")
	case "top":
		fs.IntVar(&cmd.limit, "limit", 10, "number of errors to show")
	case "recent":
		fs.IntVar(&cmd.hours, "hours", 1, "look back N hours")
	case "correlate":
		fs.IntVar(&cmd.nth, "nth", 1, "occurrence index, starting at 1")
	case "silence":
		fs.StringVar(&cmd.reason, "reason", "", "reason for silencing")
	case "show", "trend", "releases", "stats", "gc", "resolve", "silences", "unsilence", "health":
	default:
		return nil, fmt.Errorf("unknown command: %s", cmd.name)
	}
	if cmd.name == "list" || cmd.name == "top" || cmd.name == "recent" {
		fs.StringVar(&cmd.filter.Level, "level", "", "filter by level")
		fs.StringVar(&tag, "tag", "", "filter by key=value")
	}
	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			_, _ = help.Write(usage.Bytes())
		}
		return nil, err
	}
	positional := fs.Args()
	if cmd.name == "backup" && cmd.output == "" {
		return nil, fmt.Errorf("backup requires --output PATH")
	}
	min, max := 0, 0
	switch cmd.name {
	case "show", "trend", "releases", "correlate", "resolve", "unsilence", "gc":
		min, max = 1, 1
	case "silence":
		min, max = 1, 2
	}
	if len(positional) < min || len(positional) > max {
		return nil, fmt.Errorf("invalid arguments for %s; usage: %s --help", cmd.name, cmd.name)
	}
	if (cmd.name == "top" && cmd.limit < 1) || (cmd.name == "recent" && (cmd.hours < 1 || cmd.hours > 8760)) || (cmd.name == "correlate" && cmd.nth < 1) {
		return nil, fmt.Errorf("limit and nth must be positive; hours must be 1–8760")
	}
	if tag != "" {
		k, v, ok := domain.ParseTag(tag)
		if !ok {
			return nil, fmt.Errorf("tag must be key=value")
		}
		cmd.filter.TagKey, cmd.filter.TagVal = k, v
	}
	if cmd.name == "list" {
		cmd.listQuery.Filter = cmd.filter
		if err := cmd.listQuery.Validate(); err != nil {
			return nil, err
		}
		if cmd.listQuery.Sort == "" {
			cmd.listQuery.Sort = domain.SortLastSeen
		}
	}
	if len(positional) > 0 && cmd.name != "gc" {
		cmd.reference = positional[0]
		if !domain.ValidFingerprint(cmd.reference) {
			return nil, domain.ErrInvalidFingerprint
		}
	}
	if cmd.name == "gc" {
		cmd.durationText = positional[0]
	} else if len(positional) == 2 {
		cmd.durationText = positional[1]
	}
	if cmd.durationText != "" {
		d, err := domain.ParseDuration(cmd.durationText)
		if err != nil {
			return nil, err
		}
		cmd.duration = d
	}
	return cmd, nil
}

func (c *Command) Run(ctx context.Context, backend *CLI, w io.Writer) error {
	switch c.name {
	case "backup":
		return backend.runBackup(ctx, c, w)
	case "list":
		return backend.runList(ctx, c, w)
	case "top":
		return backend.runTop(ctx, c, w)
	case "recent":
		return backend.runRecent(ctx, c, w)
	case "show":
		return backend.runShow(ctx, c, w)
	case "trend":
		return backend.runTrend(ctx, c, w)
	case "releases":
		return backend.runReleases(ctx, c, w)
	case "correlate":
		return backend.runCorrelate(ctx, c, w)
	case "stats":
		return backend.runStats(ctx, c, w)
	case "gc":
		return backend.runGC(ctx, c, w)
	case "resolve":
		return backend.runResolve(ctx, c, w)
	case "silence":
		return backend.runSilence(ctx, c, w)
	case "silences":
		return backend.runSilences(ctx, c, w)
	case "unsilence":
		return backend.runUnsilence(ctx, c, w)
	default:
		return fmt.Errorf("%s requires its dedicated backend", c.name)
	}
}
