package main

import (
	"flag"
	"fmt"
	"io"
	"time"
)

func runCorrelate(args []string, w io.Writer, cfg Config) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip correlate <fingerprint>")
		return
	}

	fs := flag.NewFlagSet("correlate", flag.ExitOnError)
	nth := fs.Int("nth", 1, "Nth most recent occurrence")
	_ = fs.Parse(args)

	fp := fs.Arg(0)
	if fp == "" {
		fmt.Fprintln(w, "usage: drillip correlate <fingerprint>")
		return
	}

	// Fetch error row
	var fullFP, typ, val, stacktrace, breadcrumbs, userCtx string
	err := db.QueryRow(`
		SELECT fingerprint, type, value, stacktrace, breadcrumbs, user_context
		FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1
	`, fp).Scan(&fullFP, &typ, &val, &stacktrace, &breadcrumbs, &userCtx)
	if err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	// Fetch Nth most recent occurrence
	var occTimestamp, occTraceID string
	err = db.QueryRow(`
		SELECT timestamp, COALESCE(trace_id, '')
		FROM occurrences WHERE fingerprint = ?
		ORDER BY timestamp DESC LIMIT 1 OFFSET ?
	`, fullFP, *nth-1).Scan(&occTimestamp, &occTraceID)

	var occTime time.Time
	if err == nil {
		occTime, _ = time.Parse(time.RFC3339, occTimestamp)
	}

	// Header
	printSection(w, "Error")
	fmt.Fprintf(w, "Type:        %s\n", typ)
	fmt.Fprintf(w, "Value:       %s\n", val)
	fmt.Fprintf(w, "Fingerprint: %s\n", fullFP)
	if !occTime.IsZero() {
		fmt.Fprintf(w, "Occurrence:  #%d at %s (%s)\n", *nth, occTimestamp, timeAgo(occTime))
	}

	// Stacktrace (always)
	if stacktrace != "" {
		fmt.Fprintln(w)
		printSection(w, "Stacktrace")
		printStacktrace(w, stacktrace)
	}

	// Logs — if unit configured
	if cfg.Unit != "" && !occTime.IsZero() {
		fmt.Fprintln(w)
		printSection(w, "Logs")
		entries, err := queryJournalctl(cfg.Unit, occTime)
		if err != nil {
			fmt.Fprintf(w, "  (unavailable: %v)\n", err)
		} else if len(entries) == 0 {
			fmt.Fprintln(w, "  (no log entries in window)")
		} else {
			for _, e := range entries {
				fmt.Fprintf(w, "  %s  %s\n", e.Timestamp, e.Message)
			}
		}
	}

	// Breadcrumbs (always)
	if breadcrumbs != "" {
		fmt.Fprintln(w)
		printSection(w, "Breadcrumbs")
		printBreadcrumbs(w, breadcrumbs)
	}

	// Trace — if VT configured and occurrence has trace_id
	if cfg.VTURL != "" && occTraceID != "" {
		fmt.Fprintln(w)
		printSection(w, "Trace")
		td, err := queryVictoriaTraces(cfg.VTURL, occTraceID)
		if err != nil {
			fmt.Fprintf(w, "  (unavailable: %v)\n", err)
		} else if td == nil {
			fmt.Fprintln(w, "  (no trace data)")
		} else {
			fmt.Fprintf(w, "  Service: %s\n", td.ServiceName)
			for _, s := range td.Spans {
				fmt.Fprintf(w, "  %s  %s\n", s.Duration, s.OperationName)
			}
		}
	}

	// Metrics — if VM configured
	if cfg.VMURL != "" && !occTime.IsZero() {
		fmt.Fprintln(w)
		printSection(w, "Metrics")
		snap, err := queryVictoriaMetrics(cfg.VMURL, occTime)
		if err != nil {
			fmt.Fprintf(w, "  (unavailable: %v)\n", err)
		} else if snap == nil || len(snap.Values) == 0 {
			fmt.Fprintln(w, "  (no metrics data)")
		} else {
			for k, v := range snap.Values {
				fmt.Fprintf(w, "  %s: %s\n", k, v)
			}
		}
	}

	// Profile — if Pyroscope configured
	if cfg.PyroscopeURL != "" && !occTime.IsZero() {
		fmt.Fprintln(w)
		printSection(w, "Profile")
		entries, err := queryPyroscope(cfg.PyroscopeURL, cfg.Service, occTime)
		if err != nil {
			fmt.Fprintf(w, "  (unavailable: %v)\n", err)
		} else if len(entries) == 0 {
			fmt.Fprintln(w, "  (no profile data)")
		} else {
			for _, e := range entries {
				fmt.Fprintf(w, "  %s\n", e.Function)
			}
		}
	}

	// User (always)
	if userCtx != "" && userCtx != "null" {
		fmt.Fprintln(w)
		printSection(w, "User")
		fmt.Fprintf(w, "  %s\n", userCtx)
	}

	// Next hints
	printHint(w, "drillip show "+fullFP[:8], "drillip trend "+fullFP[:8])
}
