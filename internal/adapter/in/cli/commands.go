package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"time"

	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/domain"
)

// CLI runs commands through application use-case ports.
type CLI struct {
	Errors      inport.Errors
	Correlation inport.Correlator
}

func (c *CLI) RunTop(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("top", flag.ContinueOnError)
	limit := fs.Int("limit", 10, "number of errors to show")
	level := fs.String("level", "", "filter by level (error, warning, info, etc.)")
	tag := fs.String("tag", "", "filter by tag (key=value)")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(w)
			fs.Usage()
		}
		return err
	}

	f := domain.ListFilter{Level: *level}
	if *tag != "" {
		if k, v, ok := domain.ParseTag(*tag); ok {
			f.TagKey = k
			f.TagVal = v
		}
	}

	summaries, err := c.Errors.ListTop(f, *limit)
	if err != nil {
		return err
	}

	if len(summaries) == 0 {
		fmt.Fprintln(w, "no errors recorded")
		return nil
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
	return nil
}

func (c *CLI) RunRecent(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("recent", flag.ContinueOnError)
	hours := fs.Int("hours", 1, "look back N hours")
	level := fs.String("level", "", "filter by level (error, warning, info, etc.)")
	tag := fs.String("tag", "", "filter by tag (key=value)")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(w)
			fs.Usage()
		}
		return err
	}

	since := time.Now().UTC().Add(-time.Duration(*hours) * time.Hour)

	f := domain.ListFilter{Level: *level}
	if *tag != "" {
		if k, v, ok := domain.ParseTag(*tag); ok {
			f.TagKey = k
			f.TagVal = v
		}
	}

	summaries, err := c.Errors.ListRecent(f, since)
	if err != nil {
		return err
	}

	if len(summaries) == 0 {
		fmt.Fprintf(w, "no new errors in the last %d hour(s)\n", *hours)
		return nil
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
	return nil
}

func (c *CLI) RunShow(args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: drillip show <fingerprint>")
	}
	fp := args[0]
	if !domain.ValidFingerprint(fp) {
		return fmt.Errorf("invalid fingerprint: must be 1-16 hex characters")
	}

	fullFP, err := c.Errors.FindByPrefix(fp)
	if err != nil {
		return fmt.Errorf("error %s: %w", fp, err)
	}

	d, err := c.Errors.GetDetail(fullFP)
	if err != nil {
		return fmt.Errorf("error %s: %w", fp, err)
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
	return nil
}

// printTagDistribution shows how tag values are distributed across occurrences.
func printTagDistribution(w io.Writer, dist map[string]domain.TagDist) {
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
		sorted := make([]domain.TagValue, len(td.Values))
		copy(sorted, td.Values)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Count > sorted[j].Count })

		fmt.Fprintf(w, "  %s:\n", k)
		for _, tv := range sorted {
			fmt.Fprintf(w, "    %s: %d (%d%%)\n", tv.Value, tv.Count, tv.Percent)
		}
	}
}

func (c *CLI) RunTrend(args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: drillip trend <fingerprint>")
	}
	fp := args[0]
	if !domain.ValidFingerprint(fp) {
		return fmt.Errorf("invalid fingerprint: must be 1-16 hex characters")
	}

	// Resolve full fingerprint
	fullFP, err := c.Errors.FindByPrefix(fp)
	if err != nil {
		return fmt.Errorf("error %s: %w", fp, err)
	}

	// Query occurrences grouped by hour for last 24h
	since := time.Now().UTC().Add(-24 * time.Hour)
	buckets, err := c.Errors.GetTrend(fullFP, since)
	if err != nil {
		return err
	}

	if len(buckets) == 0 {
		fmt.Fprintf(w, "no occurrences in the last 24h for %s\n", fullFP[:8])
		return nil
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
	return nil
}

func (c *CLI) RunCorrelate(args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: drillip correlate <fingerprint>")
	}

	fs := flag.NewFlagSet("correlate", flag.ContinueOnError)
	nth := fs.Int("nth", 1, "Nth most recent occurrence")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(w)
			fs.Usage()
		}
		return err
	}

	fp := fs.Arg(0)
	if fp == "" {
		return fmt.Errorf("usage: drillip correlate <fingerprint>")
	}
	if !domain.ValidFingerprint(fp) {
		return fmt.Errorf("invalid fingerprint: must be 1-16 hex characters")
	}

	cr, err := c.Correlation.Correlate(inport.CorrelateQuery{Fingerprint: fp, Nth: *nth})
	if err != nil {
		return fmt.Errorf("error %s: %w", fp, err)
	}
	cd := cr.Error
	fullFP := cd.Fingerprint

	// Header
	printSection(w, "Error")
	fmt.Fprintf(w, "Type:        %s\n", cd.Type)
	fmt.Fprintf(w, "Value:       %s\n", cd.Value)
	fmt.Fprintf(w, "Fingerprint: %s\n", fullFP)
	if occ := cr.Occurrence; occ != nil && !occ.Time.IsZero() {
		fmt.Fprintf(w, "Occurrence:  #%d at %s (%s)\n", occ.Nth, occ.Timestamp, timeAgo(occ.Time))
	}

	// Stacktrace (always)
	if cd.Stacktrace != "" {
		fmt.Fprintln(w)
		printSection(w, "Stacktrace")
		printStacktrace(w, cd.Stacktrace)
	}

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
	return nil
}

func (c *CLI) RunReleases(args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: drillip releases <fingerprint>")
	}
	fp := args[0]
	if !domain.ValidFingerprint(fp) {
		return fmt.Errorf("invalid fingerprint: must be 1-16 hex characters")
	}

	// Resolve full fingerprint
	fullFP, err := c.Errors.FindByPrefix(fp)
	if err != nil {
		return fmt.Errorf("error %s: %w", fp, err)
	}

	releases, err := c.Errors.GetReleases(fullFP)
	if err != nil {
		return err
	}

	if len(releases) == 0 {
		fmt.Fprintf(w, "no occurrences for %s\n", fullFP[:8])
		return nil
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
	return nil
}

func (c *CLI) RunStats(_ []string, w io.Writer) error {
	st, err := c.Errors.GetStats()
	if err != nil {
		return err
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
	return nil
}

func (c *CLI) RunGC(args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: drillip gc <duration> (e.g., 7d, 30d, 24h)")
	}

	dur, err := domain.ParseDuration(args[0])
	if err != nil {
		return err
	}

	deleted, err := c.Errors.GCOccurrences(time.Now().UTC().Add(-dur))
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "deleted %d occurrences older than %s\n", deleted, args[0])
	return nil
}

func (c *CLI) RunResolve(args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: drillip resolve <fingerprint>")
	}
	fpPrefix := args[0]
	if !domain.ValidFingerprint(fpPrefix) {
		return fmt.Errorf("invalid fingerprint: must be 1-16 hex characters")
	}

	result, err := c.Errors.Resolve(fpPrefix)
	if err != nil {
		return err
	}
	if result.Matched == 0 {
		return fmt.Errorf("no unresolved error matching %s", fpPrefix)
	}
	fmt.Fprintf(w, "resolved %d error(s) matching %s\n", result.Matched, fpPrefix)
	return nil
}

func (c *CLI) RunSilence(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("silence", flag.ContinueOnError)
	reason := fs.String("reason", "", "reason for silencing")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(w)
			fs.Usage()
		}
		return err
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return fmt.Errorf("usage: drillip silence <fingerprint> [duration] [--reason \"...\"]")
	}

	fp := remaining[0]
	if !domain.ValidFingerprint(fp) {
		return fmt.Errorf("invalid fingerprint: must be 1-16 hex characters")
	}

	var expiresAt *time.Time
	if len(remaining) > 1 {
		dur, err := domain.ParseDuration(remaining[1])
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
		t := time.Now().UTC().Add(dur)
		expiresAt = &t
	}

	if err := c.Errors.Silence(fp, expiresAt, *reason); err != nil {
		return err
	}

	if expiresAt != nil {
		fmt.Fprintf(w, "silenced %s until %s\n", fp, expiresAt.Format(time.RFC3339))
	} else {
		fmt.Fprintf(w, "silenced %s permanently\n", fp)
	}
	return nil
}

func (c *CLI) RunSilences(_ []string, w io.Writer) error {
	entries, err := c.Errors.ListSilences()
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Fprintln(w, "no active silences")
		return nil
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
	return nil
}

func (c *CLI) RunUnsilence(args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: drillip unsilence <fingerprint>")
	}
	fp := args[0]
	if !domain.ValidFingerprint(fp) {
		return fmt.Errorf("invalid fingerprint: must be 1-16 hex characters")
	}
	if err := c.Errors.Unsilence(fp); err != nil {
		return err
	}
	fmt.Fprintf(w, "unsilenced %s\n", fp)
	return nil
}
