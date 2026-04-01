package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/PhilHem/drillip/domain"
	"github.com/PhilHem/drillip/integrations"
	"github.com/PhilHem/drillip/store"
)

// CLI holds the store connection for CLI commands.
type CLI struct {
	Store *store.Store
}

func (c *CLI) RunTop(args []string, w io.Writer) {
	fs := flag.NewFlagSet("top", flag.ExitOnError)
	limit := fs.Int("limit", 10, "number of errors to show")
	level := fs.String("level", "", "filter by level (error, warning, info, etc.)")
	tag := fs.String("tag", "", "filter by tag (key=value)")
	_ = fs.Parse(args)

	f := store.ListFilter{Level: *level}
	if *tag != "" {
		if k, v, ok := domain.ParseTag(*tag); ok {
			f.TagKey = k
			f.TagVal = v
		}
	}

	summaries, err := c.Store.ListTop(f, *limit)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}

	if len(summaries) == 0 {
		fmt.Fprintln(w, "no errors recorded")
		return
	}

	var tableRows [][]string
	for _, e := range summaries {
		t, _ := time.Parse(time.RFC3339, e.LastSeen)
		tableRows = append(tableRows, []string{
			e.Fingerprint[:8], fmt.Sprintf("%d", e.Count), e.Level, e.State, e.Type, truncate(e.Value, 50), timeAgo(t),
		})
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

	since := time.Now().UTC().Add(-time.Duration(*hours) * time.Hour)

	f := store.ListFilter{Level: *level}
	if *tag != "" {
		if k, v, ok := domain.ParseTag(*tag); ok {
			f.TagKey = k
			f.TagVal = v
		}
	}

	summaries, err := c.Store.ListRecent(f, since)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}

	if len(summaries) == 0 {
		fmt.Fprintf(w, "no new errors in the last %d hour(s)\n", *hours)
		return
	}

	var tableRows [][]string
	for _, e := range summaries {
		t, _ := time.Parse(time.RFC3339, e.FirstSeen)
		tableRows = append(tableRows, []string{
			e.Fingerprint[:8], fmt.Sprintf("%d", e.Count), e.Level, e.State, e.Type, truncate(e.Value, 50), timeAgo(t),
		})
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
	if !domain.ValidFingerprint(fp) {
		fmt.Fprintln(w, "invalid fingerprint: must be 1-16 hex characters")
		return
	}

	fullFP, err := c.Store.FindByPrefix(fp)
	if err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	d, err := c.Store.GetDetail(fullFP)
	if err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	first, _ := time.Parse(time.RFC3339, d.FirstSeen)
	last, _ := time.Parse(time.RFC3339, d.LastSeen)

	printSection(w, "Error")
	fmt.Fprintf(w, "Fingerprint: %s\n", fullFP)
	fmt.Fprintf(w, "Level:       %s\n", d.Level)
	fmt.Fprintf(w, "Type:        %s\n", d.Type)
	fmt.Fprintf(w, "Value:       %s\n", d.Value)
	fmt.Fprintf(w, "Count:       %d\n", d.Count)
	fmt.Fprintf(w, "First seen:  %s (%s)\n", d.FirstSeen, timeAgo(first))
	fmt.Fprintf(w, "Last seen:   %s (%s)\n", d.LastSeen, timeAgo(last))
	if d.Release != "" {
		fmt.Fprintf(w, "Release:     %s\n", d.Release)
	}
	if d.Environment != "" {
		fmt.Fprintf(w, "Environment: %s\n", d.Environment)
	}
	if d.Platform != "" {
		fmt.Fprintf(w, "Platform:    %s\n", d.Platform)
	}

	if d.Stacktrace != "" {
		fmt.Fprintln(w)
		printSection(w, "Stacktrace")
		printStacktrace(w, d.Stacktrace)
	}

	if d.Breadcrumbs != "" {
		fmt.Fprintln(w)
		printSection(w, "Breadcrumbs")
		printBreadcrumbs(w, d.Breadcrumbs)
	}

	if d.UserContext != "" && d.UserContext != "null" {
		fmt.Fprintln(w)
		printSection(w, "User")
		var user map[string]interface{}
		if json.Unmarshal([]byte(d.UserContext), &user) == nil {
			for k, v := range user {
				fmt.Fprintf(w, "  %s: %v\n", k, v)
			}
		}
	}

	if d.Tags != "" && d.Tags != "null" {
		fmt.Fprintln(w)
		printSection(w, "Tags")
		var tagMap map[string]string
		if json.Unmarshal([]byte(d.Tags), &tagMap) == nil {
			for k, v := range tagMap {
				fmt.Fprintf(w, "  %s: %s\n", k, v)
			}
		}
	}

	// Tag distribution from occurrences
	printTagDistribution(w, d.TagDist)

	printHint(w, "drillip trend "+fullFP[:8], "drillip correlate "+fullFP[:8],
		"drillip top --tag key=value")
}

// printTagDistribution shows how tag values are distributed across occurrences.
func printTagDistribution(w io.Writer, dist map[string]store.TagDist) {
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
		td := dist[k]
		// Sort values by count descending
		sorted := make([]store.TagValue, len(td.Values))
		copy(sorted, td.Values)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Count > sorted[j].Count })

		fmt.Fprintf(w, "  %s:\n", k)
		for _, tv := range sorted {
			fmt.Fprintf(w, "    %s: %d (%d%%)\n", tv.Value, tv.Count, tv.Percent)
		}
	}
}

func (c *CLI) RunTrend(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip trend <fingerprint>")
		return
	}
	fp := args[0]
	if !domain.ValidFingerprint(fp) {
		fmt.Fprintln(w, "invalid fingerprint: must be 1-16 hex characters")
		return
	}

	// Resolve full fingerprint
	fullFP, err := c.Store.FindByPrefix(fp)
	if err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	// Query occurrences grouped by hour for last 24h
	since := time.Now().UTC().Add(-24 * time.Hour)
	buckets, err := c.Store.GetTrend(fullFP, since)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}

	if len(buckets) == 0 {
		fmt.Fprintf(w, "no occurrences in the last 24h for %s\n", fullFP[:8])
		return
	}

	maxCount := 0
	for _, b := range buckets {
		if b.Count > maxCount {
			maxCount = b.Count
		}
	}

	fmt.Fprintf(w, "Trend (last 24h) for %s:\n\n", fullFP[:8])
	for _, b := range buckets {
		// Show just the hour part
		label := b.Hour[11:16]
		printBar(w, label, b.Count, maxCount, 30)
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
	if !domain.ValidFingerprint(fp) {
		fmt.Fprintln(w, "invalid fingerprint: must be 1-16 hex characters")
		return
	}

	// Resolve full fingerprint
	fullFP, err := c.Store.FindByPrefix(fp)
	if err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	// Fetch error row
	cd, err := c.Store.GetCorrelateData(fullFP)
	if err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	// Fetch Nth most recent occurrence
	var occTimestamp, occTraceID string
	var occTime time.Time
	occ, occErr := c.Store.GetNthOccurrence(fullFP, *nth)
	if occErr == nil {
		occTimestamp = occ.Timestamp
		occTraceID = occ.TraceID
		occTime, _ = time.Parse(time.RFC3339, occTimestamp)
	}

	// Header
	printSection(w, "Error")
	fmt.Fprintf(w, "Type:        %s\n", cd.Type)
	fmt.Fprintf(w, "Value:       %s\n", cd.Value)
	fmt.Fprintf(w, "Fingerprint: %s\n", fullFP)
	if !occTime.IsZero() {
		fmt.Fprintf(w, "Occurrence:  #%d at %s (%s)\n", *nth, occTimestamp, timeAgo(occTime))
	}

	// Stacktrace (always)
	if cd.Stacktrace != "" {
		fmt.Fprintln(w)
		printSection(w, "Stacktrace")
		printStacktrace(w, cd.Stacktrace)
	}

	cr := integrations.Correlate(cfg, occTime, occTraceID)

	if len(cr.Logs) > 0 {
		fmt.Fprintln(w)
		printSection(w, "Logs")
		for _, e := range cr.Logs {
			fmt.Fprintf(w, "  %s  %s\n", e.Timestamp, e.Message)
		}
	}

	// Breadcrumbs (always)
	if cd.Breadcrumbs != "" {
		fmt.Fprintln(w)
		printSection(w, "Breadcrumbs")
		printBreadcrumbs(w, cd.Breadcrumbs)
	}

	if cr.Trace != nil {
		fmt.Fprintln(w)
		printSection(w, "Trace")
		fmt.Fprintf(w, "  Service: %s\n", cr.Trace.ServiceName)
		for _, s := range cr.Trace.Spans {
			fmt.Fprintf(w, "  %s  %s\n", s.Duration, s.OperationName)
		}
	}

	if cr.Metrics != nil && len(cr.Metrics.Values) > 0 {
		fmt.Fprintln(w)
		printSection(w, "Metrics")
		for k, v := range cr.Metrics.Values {
			fmt.Fprintf(w, "  %s: %s\n", k, v)
		}
	}

	if len(cr.Profile) > 0 {
		fmt.Fprintln(w)
		printSection(w, "Profile")
		for _, e := range cr.Profile {
			fmt.Fprintf(w, "  %s\n", e.Function)
		}
	}

	// User (always)
	if cd.UserContext != "" && cd.UserContext != "null" {
		fmt.Fprintln(w)
		printSection(w, "User")
		fmt.Fprintf(w, "  %s\n", cd.UserContext)
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
	if !domain.ValidFingerprint(fp) {
		fmt.Fprintln(w, "invalid fingerprint: must be 1-16 hex characters")
		return
	}

	// Resolve full fingerprint
	fullFP, err := c.Store.FindByPrefix(fp)
	if err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	releases, err := c.Store.GetReleases(fullFP)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}

	if len(releases) == 0 {
		fmt.Fprintf(w, "no occurrences for %s\n", fullFP[:8])
		return
	}

	var tableRows [][]string
	for _, r := range releases {
		release := r.Release
		if release == "" {
			release = "(none)"
		}
		tableRows = append(tableRows, []string{
			release, fmt.Sprintf("%d", r.Count), r.FirstSeen, r.LastSeen,
		})
	}

	fmt.Fprintf(w, "Releases for %s:\n\n", fullFP[:8])
	printTable(w, []string{"RELEASE", "COUNT", "FIRST SEEN", "LAST SEEN"}, tableRows)
}

func (c *CLI) RunStats(_ []string, w io.Writer) {
	st, err := c.Store.GetStats()
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}

	fmt.Fprintf(w, "Unique errors:      %d\n", st.UniqueErrors)
	fmt.Fprintf(w, "Total occurrences:  %d\n", st.TotalOccurrences)
	if st.FirstSeen != "" {
		fmt.Fprintf(w, "First seen:         %s\n", st.FirstSeen)
	}
	if st.LastSeen != "" {
		fmt.Fprintf(w, "Last seen:          %s\n", st.LastSeen)
	}

	printHint(w, "drillip top")
}

func (c *CLI) RunGC(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip gc <duration> (e.g., 7d, 30d, 24h)")
		return
	}

	dur, err := domain.ParseDuration(args[0])
	if err != nil {
		fmt.Fprintf(w, "%v\n", err)
		return
	}

	deleted, err := c.Store.GCOccurrences(time.Now().UTC().Add(-dur))
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	fmt.Fprintf(w, "deleted %d occurrences older than %s\n", deleted, args[0])
}

func (c *CLI) RunResolve(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip resolve <fingerprint>")
		return
	}
	fpPrefix := args[0]
	if !domain.ValidFingerprint(fpPrefix) {
		fmt.Fprintln(w, "invalid fingerprint: must be 1-16 hex characters")
		return
	}

	result, err := c.Store.Resolve(fpPrefix)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	if result.Matched == 0 {
		fmt.Fprintf(w, "no unresolved error matching %s\n", fpPrefix)
		return
	}
	fmt.Fprintf(w, "resolved %d error(s) matching %s\n", result.Matched, fpPrefix)
}

func (c *CLI) RunSilence(args []string, w io.Writer) {
	fs := flag.NewFlagSet("silence", flag.ExitOnError)
	reason := fs.String("reason", "", "reason for silencing")
	_ = fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) == 0 {
		fmt.Fprintln(w, "usage: drillip silence <fingerprint> [duration] [--reason \"...\"]")
		return
	}

	fp := remaining[0]
	if !domain.ValidFingerprint(fp) {
		fmt.Fprintln(w, "invalid fingerprint: must be 1-16 hex characters")
		return
	}

	var expiresAt *time.Time
	if len(remaining) > 1 {
		dur, err := domain.ParseDuration(remaining[1])
		if err != nil {
			fmt.Fprintf(w, "invalid duration: %v\n", err)
			return
		}
		t := time.Now().UTC().Add(dur)
		expiresAt = &t
	}

	if err := c.Store.Silence(fp, expiresAt, *reason); err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}

	if expiresAt != nil {
		fmt.Fprintf(w, "silenced %s until %s\n", fp, expiresAt.Format(time.RFC3339))
	} else {
		fmt.Fprintf(w, "silenced %s permanently\n", fp)
	}
}

func (c *CLI) RunSilences(_ []string, w io.Writer) {
	entries, err := c.Store.ListSilences()
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}

	if len(entries) == 0 {
		fmt.Fprintln(w, "no active silences")
		return
	}

	var tableRows [][]string
	for _, e := range entries {
		expires := e.ExpiresAt
		if expires == "" {
			expires = "permanent"
		}
		tableRows = append(tableRows, []string{e.Fingerprint, e.CreatedAt, expires, e.Reason})
	}

	printTable(w, []string{"FINGERPRINT", "CREATED", "EXPIRES", "REASON"}, tableRows)
}

func (c *CLI) RunUnsilence(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip unsilence <fingerprint>")
		return
	}
	fp := args[0]
	if !domain.ValidFingerprint(fp) {
		fmt.Fprintln(w, "invalid fingerprint: must be 1-16 hex characters")
		return
	}
	if err := c.Store.Unsilence(fp); err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	fmt.Fprintf(w, "unsilenced %s\n", fp)
}
