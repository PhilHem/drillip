package cli

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PhilHem/drillip/integrations"
)

// CLI holds the database connection for CLI commands.
type CLI struct {
	DB *sql.DB
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	suffix := s[len(s)-1]
	numStr := s[:len(s)-1]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	switch suffix {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown suffix %q (use h/d/w)", string(suffix))
	}
}

// deriveState returns "resolved", "new", or "ongoing" based on resolved_at and first_seen.
func deriveState(resolvedAt, firstSeen string) string {
	if resolvedAt != "" {
		return "resolved"
	}
	if t, err := time.Parse(time.RFC3339, firstSeen); err == nil {
		if time.Since(t) < time.Hour {
			return "new"
		}
	}
	return "ongoing"
}

func (c *CLI) RunTop(args []string, w io.Writer) {
	fs := flag.NewFlagSet("top", flag.ExitOnError)
	limit := fs.Int("limit", 10, "number of errors to show")
	level := fs.String("level", "", "filter by level (error, warning, info, etc.)")
	tag := fs.String("tag", "", "filter by tag (key=value)")
	_ = fs.Parse(args)

	query := `SELECT fingerprint, count, type, value, level, last_seen, first_seen, COALESCE(resolved_at, '') FROM errors`
	var conditions []string
	var queryArgs []interface{}
	if *level != "" {
		conditions = append(conditions, `level = ?`)
		queryArgs = append(queryArgs, *level)
	}
	if *tag != "" {
		if k, v, ok := parseTag(*tag); ok {
			conditions = append(conditions, `json_extract(tags, '$.'||?) = ?`)
			queryArgs = append(queryArgs, k, v)
		}
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	query += ` ORDER BY count DESC LIMIT ?`
	queryArgs = append(queryArgs, *limit)

	rows, err := c.DB.Query(query, queryArgs...)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	defer rows.Close()

	var tableRows [][]string
	for rows.Next() {
		var fp, typ, val, lvl, lastSeen, firstSeen, resolvedAt string
		var count int
		if err := rows.Scan(&fp, &count, &typ, &val, &lvl, &lastSeen, &firstSeen, &resolvedAt); err != nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339, lastSeen)
		state := deriveState(resolvedAt, firstSeen)
		tableRows = append(tableRows, []string{
			fp[:8], fmt.Sprintf("%d", count), lvl, state, typ, truncate(val, 50), timeAgo(t),
		})
	}

	if len(tableRows) == 0 {
		fmt.Fprintln(w, "no errors recorded")
		return
	}

	printTable(w, []string{"FINGERPRINT", "COUNT", "LEVEL", "STATE", "TYPE", "VALUE", "LAST SEEN"}, tableRows)
	printHint(w, "drillip show <fingerprint>")
}

func (c *CLI) RunRecent(args []string, w io.Writer) {
	fs := flag.NewFlagSet("recent", flag.ExitOnError)
	hours := fs.Int("hours", 1, "look back N hours")
	level := fs.String("level", "", "filter by level (error, warning, info, etc.)")
	tag := fs.String("tag", "", "filter by tag (key=value)")
	_ = fs.Parse(args)

	since := time.Now().UTC().Add(-time.Duration(*hours) * time.Hour).Format(time.RFC3339)

	query := `SELECT fingerprint, count, type, value, level, first_seen, COALESCE(resolved_at, '') FROM errors WHERE first_seen > ?`
	queryArgs := []interface{}{since}
	if *level != "" {
		query += ` AND level = ?`
		queryArgs = append(queryArgs, *level)
	}
	if *tag != "" {
		if k, v, ok := parseTag(*tag); ok {
			query += ` AND json_extract(tags, '$.'||?) = ?`
			queryArgs = append(queryArgs, k, v)
		}
	}
	query += ` ORDER BY first_seen DESC`

	rows, err := c.DB.Query(query, queryArgs...)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	defer rows.Close()

	var tableRows [][]string
	for rows.Next() {
		var fp, typ, val, lvl, firstSeen, resolvedAt string
		var count int
		if err := rows.Scan(&fp, &count, &typ, &val, &lvl, &firstSeen, &resolvedAt); err != nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339, firstSeen)
		state := deriveState(resolvedAt, firstSeen)
		tableRows = append(tableRows, []string{
			fp[:8], fmt.Sprintf("%d", count), lvl, state, typ, truncate(val, 50), timeAgo(t),
		})
	}

	if len(tableRows) == 0 {
		fmt.Fprintf(w, "no new errors in the last %d hour(s)\n", *hours)
		return
	}

	fmt.Fprintf(w, "New errors (last %dh):\n\n", *hours)
	printTable(w, []string{"FINGERPRINT", "COUNT", "LEVEL", "STATE", "TYPE", "VALUE", "FIRST SEEN"}, tableRows)
	printHint(w, "drillip show <fingerprint>")
}

func (c *CLI) RunShow(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip show <fingerprint>")
		return
	}
	fp := args[0]

	var fullFP, typ, val, lvl, stacktrace, breadcrumbs, release, env, userCtx, tags, platform, firstSeen, lastSeen string
	var count int
	err := c.DB.QueryRow(`
		SELECT fingerprint, type, value, level, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform,
			first_seen, last_seen, count
		FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1
	`, fp).Scan(&fullFP, &typ, &val, &lvl, &stacktrace, &breadcrumbs,
		&release, &env, &userCtx, &tags, &platform,
		&firstSeen, &lastSeen, &count)
	if err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	first, _ := time.Parse(time.RFC3339, firstSeen)
	last, _ := time.Parse(time.RFC3339, lastSeen)

	printSection(w, "Error")
	fmt.Fprintf(w, "Fingerprint: %s\n", fullFP)
	fmt.Fprintf(w, "Level:       %s\n", lvl)
	fmt.Fprintf(w, "Type:        %s\n", typ)
	fmt.Fprintf(w, "Value:       %s\n", val)
	fmt.Fprintf(w, "Count:       %d\n", count)
	fmt.Fprintf(w, "First seen:  %s (%s)\n", firstSeen, timeAgo(first))
	fmt.Fprintf(w, "Last seen:   %s (%s)\n", lastSeen, timeAgo(last))
	if release != "" {
		fmt.Fprintf(w, "Release:     %s\n", release)
	}
	if env != "" {
		fmt.Fprintf(w, "Environment: %s\n", env)
	}
	if platform != "" {
		fmt.Fprintf(w, "Platform:    %s\n", platform)
	}

	if stacktrace != "" {
		fmt.Fprintln(w)
		printSection(w, "Stacktrace")
		printStacktrace(w, stacktrace)
	}

	if breadcrumbs != "" {
		fmt.Fprintln(w)
		printSection(w, "Breadcrumbs")
		printBreadcrumbs(w, breadcrumbs)
	}

	if userCtx != "" && userCtx != "null" {
		fmt.Fprintln(w)
		printSection(w, "User")
		var user map[string]interface{}
		if json.Unmarshal([]byte(userCtx), &user) == nil {
			for k, v := range user {
				fmt.Fprintf(w, "  %s: %v\n", k, v)
			}
		}
	}

	if tags != "" && tags != "null" {
		fmt.Fprintln(w)
		printSection(w, "Tags")
		var tagMap map[string]string
		if json.Unmarshal([]byte(tags), &tagMap) == nil {
			for k, v := range tagMap {
				fmt.Fprintf(w, "  %s: %s\n", k, v)
			}
		}
	}

	// Tag distribution from occurrences
	c.printTagDistribution(w, fullFP)

	printHint(w, "drillip trend "+fullFP[:8], "drillip correlate "+fullFP[:8],
		"drillip top --tag key=value")
}

// printTagDistribution shows how tag values are distributed across occurrences.
func (c *CLI) printTagDistribution(w io.Writer, fp string) {
	rows, err := c.DB.Query(`SELECT tags FROM occurrences WHERE fingerprint = ? AND tags != '' AND tags IS NOT NULL`, fp)
	if err != nil {
		return
	}
	defer rows.Close()

	// key -> value -> count
	dist := map[string]map[string]int{}
	total := 0

	for rows.Next() {
		var tagsJSON string
		if err := rows.Scan(&tagsJSON); err != nil || tagsJSON == "" {
			continue
		}
		var tagMap map[string]string
		if json.Unmarshal([]byte(tagsJSON), &tagMap) != nil {
			continue
		}
		total++
		for k, v := range tagMap {
			if dist[k] == nil {
				dist[k] = map[string]int{}
			}
			dist[k][v]++
		}
	}

	if len(dist) == 0 {
		return
	}

	fmt.Fprintln(w)
	printSection(w, "Tag Distribution")

	// Sort keys for stable output
	keys := make([]string, 0, len(dist))
	for k := range dist {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		values := dist[k]
		// Sort values by count descending
		type kv struct {
			val   string
			count int
		}
		sorted := make([]kv, 0, len(values))
		for v, cnt := range values {
			sorted = append(sorted, kv{v, cnt})
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })

		fmt.Fprintf(w, "  %s:\n", k)
		for _, s := range sorted {
			pct := 0
			if total > 0 {
				pct = s.count * 100 / total
			}
			fmt.Fprintf(w, "    %s: %d (%d%%)\n", s.val, s.count, pct)
		}
	}
}

func (c *CLI) RunTrend(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip trend <fingerprint>")
		return
	}
	fp := args[0]

	// Resolve full fingerprint
	var fullFP string
	if err := c.DB.QueryRow("SELECT fingerprint FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1", fp).Scan(&fullFP); err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	// Query occurrences grouped by hour for last 24h
	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	rows, err := c.DB.Query(`
		SELECT strftime('%Y-%m-%d %H:00', timestamp) AS hour, COUNT(*) AS cnt
		FROM occurrences
		WHERE fingerprint = ? AND timestamp > ?
		GROUP BY hour ORDER BY hour
	`, fullFP, since)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	defer rows.Close()

	type bucket struct {
		hour  string
		count int
	}
	var buckets []bucket
	maxCount := 0
	for rows.Next() {
		var b bucket
		if err := rows.Scan(&b.hour, &b.count); err != nil {
			continue
		}
		if b.count > maxCount {
			maxCount = b.count
		}
		buckets = append(buckets, b)
	}

	if len(buckets) == 0 {
		fmt.Fprintf(w, "no occurrences in the last 24h for %s\n", fullFP[:8])
		return
	}

	fmt.Fprintf(w, "Trend (last 24h) for %s:\n\n", fullFP[:8])
	for _, b := range buckets {
		// Show just the hour part
		label := b.hour[11:16]
		printBar(w, label, b.count, maxCount, 30)
	}

	printHint(w, "drillip correlate "+fullFP[:8])
}

func (c *CLI) RunCorrelate(args []string, w io.Writer, cfg integrations.Config) {
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
	err := c.DB.QueryRow(`
		SELECT fingerprint, type, value, stacktrace, breadcrumbs, user_context
		FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1
	`, fp).Scan(&fullFP, &typ, &val, &stacktrace, &breadcrumbs, &userCtx)
	if err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	// Fetch Nth most recent occurrence
	var occTimestamp, occTraceID string
	err = c.DB.QueryRow(`
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
		entries, err := integrations.QueryJournalctl(cfg.Unit, occTime)
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
		td, err := integrations.QueryVictoriaTraces(cfg.VTURL, occTraceID)
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
		snap, err := integrations.QueryVictoriaMetrics(cfg.VMURL, occTime)
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
		entries, err := integrations.QueryPyroscope(cfg.PyroscopeURL, cfg.Service, occTime)
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

func (c *CLI) RunReleases(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip releases <fingerprint>")
		return
	}
	fp := args[0]

	// Resolve full fingerprint
	var fullFP string
	if err := c.DB.QueryRow("SELECT fingerprint FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1", fp).Scan(&fullFP); err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	rows, err := c.DB.Query(`
		SELECT COALESCE(release_tag, '(none)'), COUNT(*),
			MIN(timestamp), MAX(timestamp)
		FROM occurrences WHERE fingerprint = ?
		GROUP BY release_tag ORDER BY COUNT(*) DESC
	`, fullFP)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	defer rows.Close()

	var tableRows [][]string
	for rows.Next() {
		var release, firstSeen, lastSeen string
		var count int
		if err := rows.Scan(&release, &count, &firstSeen, &lastSeen); err != nil {
			continue
		}
		tableRows = append(tableRows, []string{
			release, fmt.Sprintf("%d", count), firstSeen, lastSeen,
		})
	}

	if len(tableRows) == 0 {
		fmt.Fprintf(w, "no occurrences for %s\n", fullFP[:8])
		return
	}

	fmt.Fprintf(w, "Releases for %s:\n\n", fullFP[:8])
	printTable(w, []string{"RELEASE", "COUNT", "FIRST SEEN", "LAST SEEN"}, tableRows)
}

func (c *CLI) RunStats(_ []string, w io.Writer) {
	var uniqueCount, totalOccurrences int
	var minTime, maxTime string

	if err := c.DB.QueryRow("SELECT COUNT(*) FROM errors").Scan(&uniqueCount); err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	if err := c.DB.QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&totalOccurrences); err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}

	_ = c.DB.QueryRow("SELECT MIN(first_seen) FROM errors").Scan(&minTime)
	_ = c.DB.QueryRow("SELECT MAX(last_seen) FROM errors").Scan(&maxTime)

	fmt.Fprintf(w, "Unique errors:      %d\n", uniqueCount)
	fmt.Fprintf(w, "Total occurrences:  %d\n", totalOccurrences)
	if minTime != "" {
		fmt.Fprintf(w, "First seen:         %s\n", minTime)
	}
	if maxTime != "" {
		fmt.Fprintf(w, "Last seen:          %s\n", maxTime)
	}

	printHint(w, "drillip top")
}

func (c *CLI) RunGC(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip gc <duration> (e.g., 7d, 30d, 24h)")
		return
	}

	dur, err := parseDuration(args[0])
	if err != nil {
		fmt.Fprintf(w, "%v\n", err)
		return
	}

	threshold := time.Now().UTC().Add(-dur).Format(time.RFC3339)

	res, err := c.DB.Exec("DELETE FROM occurrences WHERE timestamp < ?", threshold)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	deleted, _ := res.RowsAffected()
	fmt.Fprintf(w, "deleted %d occurrences older than %s\n", deleted, args[0])
}

func (c *CLI) RunResolve(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip resolve <fingerprint>")
		return
	}
	fpPrefix := args[0]
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := c.DB.Exec(
		`UPDATE errors SET resolved_at = ? WHERE fingerprint LIKE ?||'%' AND resolved_at IS NULL`,
		now, fpPrefix,
	)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fmt.Fprintf(w, "no unresolved error matching %s\n", fpPrefix)
		return
	}
	fmt.Fprintf(w, "resolved %d error(s) matching %s\n", n, fpPrefix)
}
