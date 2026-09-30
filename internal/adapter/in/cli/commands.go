package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/domain"
)

// CLI runs commands through application use-case ports.
type CLI struct {
	Errors      inport.Errors
	Correlation inport.Correlator
	Backups     inport.Backups
	Health      inport.ServerHealth
	// CommandPrefix preserves the selected server in pagination hints.
	// An empty prefix defaults to drillip.
	CommandPrefix []string
}

func (c *CLI) runList(ctx context.Context, cmd *Command, w io.Writer) error {
	page, err := c.Errors.List(ctx, cmd.listQuery)
	if err != nil {
		return err
	}
	if len(page.Errors) == 0 {
		fmt.Fprintln(w, "no matching errors on this page")
		return nil
	}
	printErrorSummaries(w, page.Errors)
	hints := []string{commandPrefix(c.CommandPrefix) + " show <fingerprint>"}
	if page.HasMore {
		query := cmd.listQuery
		query.Offset += len(page.Errors)
		hints = append(hints, listPageCommand(query, c.CommandPrefix))
	}
	printHint(w, hints...)
	return nil
}

func listPageCommand(query domain.ListQuery, prefix []string) string {
	command := fmt.Sprintf("%s list --sort %s --limit %d --offset %d", commandPrefix(prefix), query.Sort, query.Limit, query.Offset)
	if query.Search != "" {
		command += " --search " + shellQuote(query.Search)
	}
	if query.Filter.Level != "" {
		command += " --level " + shellQuote(query.Filter.Level)
	}
	if query.Filter.TagKey != "" {
		command += " --tag " + shellQuote(query.Filter.TagKey+"="+query.Filter.TagVal)
	}
	return command
}

func commandPrefix(prefix []string) string {
	if len(prefix) == 0 {
		return "drillip"
	}
	words := make([]string, len(prefix))
	for i, word := range prefix {
		words[i] = shellQuote(word)
	}
	return strings.Join(words, " ")
}

func shellQuote(value string) string {
	if value != "" && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return value
	}
	// Single quotes keep arbitrary values literal in a POSIX shell. Close and
	// reopen the quoted string around any embedded single quote.
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (c *CLI) runTop(ctx context.Context, cmd *Command, w io.Writer) error {
	f, limit := cmd.filter, cmd.limit
	summaries, err := c.Errors.ListTop(ctx, f, limit)
	if err != nil {
		return err
	}

	if len(summaries) == 0 {
		fmt.Fprintln(w, "no errors recorded")
		return nil
	}

	printErrorSummaries(w, summaries)
	printHint(w, "drillip show <fingerprint>")
	return nil
}

func printErrorSummaries(w io.Writer, summaries []domain.ErrorSummary) {
	var tableRows [][]string
	for _, e := range summaries {
		t, _ := time.Parse(time.RFC3339, e.LastSeen)
		tableRows = append(tableRows, []string{
			e.Fingerprint, fmt.Sprintf("%d", e.Count), e.Level, e.State, e.Type, truncate(e.Value, 50), timeAgo(t),
		})
	}

	printTable(w, []string{"FINGERPRINT", "COUNT", "LEVEL", "STATE", "TYPE", "VALUE", "LAST SEEN"}, tableRows)
}

func (c *CLI) runRecent(ctx context.Context, cmd *Command, w io.Writer) error {
	hours, f := cmd.hours, cmd.filter
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	summaries, err := c.Errors.ListRecent(ctx, f, since)
	if err != nil {
		return err
	}

	if len(summaries) == 0 {
		fmt.Fprintf(w, "no new errors in the last %d hour(s)\n", hours)
		return nil
	}

	var tableRows [][]string
	for _, e := range summaries {
		t, _ := time.Parse(time.RFC3339, e.FirstSeen)
		tableRows = append(tableRows, []string{
			e.Fingerprint, fmt.Sprintf("%d", e.Count), e.Level, e.State, e.Type, truncate(e.Value, 50), timeAgo(t),
		})
	}

	fmt.Fprintf(w, "New errors (last %dh):\n\n", hours)
	printTable(w, []string{"FINGERPRINT", "COUNT", "LEVEL", "STATE", "TYPE", "VALUE", "FIRST SEEN"}, tableRows)
	printHint(w, "drillip show <fingerprint>")
	return nil
}

func (c *CLI) runShow(ctx context.Context, cmd *Command, w io.Writer) error {
	fp := cmd.reference
	d, err := c.Errors.GetDetail(ctx, fp)
	if err != nil {
		return fmt.Errorf("error %s: %w", fp, err)
	}

	fullFP := d.Fingerprint
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

	printHint(w, "drillip trend "+fullFP, "drillip correlate "+fullFP,
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

func (c *CLI) runTrend(ctx context.Context, cmd *Command, w io.Writer) error {
	fp := cmd.reference
	// Query occurrences grouped by hour for last 24h
	since := time.Now().UTC().Add(-24 * time.Hour)
	trend, err := c.Errors.GetTrend(ctx, fp, since)
	if err != nil {
		return err
	}

	fullFP, buckets := trend.Fingerprint, trend.Buckets
	if len(buckets) == 0 {
		fmt.Fprintf(w, "no occurrences in the last 24h for %s\n", fullFP)
		return nil
	}

	maxCount := 0
	for _, b := range buckets {
		if b.Count > maxCount {
			maxCount = b.Count
		}
	}

	fmt.Fprintf(w, "Trend (last 24h) for %s:\n\n", fullFP)
	for _, b := range buckets {
		// Show just the hour part
		label := b.Hour[11:16]
		printBar(w, label, b.Count, maxCount, 30)
	}

	printHint(w, "drillip correlate "+fullFP)
	return nil
}

func (c *CLI) runCorrelate(ctx context.Context, cmd *Command, w io.Writer) error {
	fp, nth := cmd.reference, cmd.nth
	cr, err := c.Correlation.Correlate(ctx, inport.CorrelateQuery{Fingerprint: fp, Nth: nth})
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
	printHint(w, "drillip show "+fullFP, "drillip trend "+fullFP)
	return nil
}

func (c *CLI) runReleases(ctx context.Context, cmd *Command, w io.Writer) error {
	fp := cmd.reference
	result, err := c.Errors.GetReleases(ctx, fp)
	if err != nil {
		return err
	}

	fullFP, releases := result.Fingerprint, result.Releases
	if len(releases) == 0 {
		fmt.Fprintf(w, "no occurrences for %s\n", fullFP)
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

	fmt.Fprintf(w, "Releases for %s:\n\n", fullFP)
	printTable(w, []string{"RELEASE", "COUNT", "FIRST SEEN", "LAST SEEN"}, tableRows)
	return nil
}

func (c *CLI) runStats(ctx context.Context, cmd *Command, w io.Writer) error {

	st, err := c.Errors.GetStats(ctx)
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

func (c *CLI) runGC(ctx context.Context, cmd *Command, w io.Writer) error {
	dur := cmd.duration
	deleted, err := c.Errors.GCOccurrences(ctx, time.Now().UTC().Add(-dur))
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "deleted %d occurrences older than %s\n", deleted, cmd.durationText)
	return nil
}

// RunResolve renders a resolution performed by the selected live or offline use case.
func (c *CLI) runResolve(ctx context.Context, cmd *Command, w io.Writer) error {
	fpPrefix := cmd.reference
	result, err := c.Errors.Resolve(ctx, fpPrefix)
	if err != nil {
		return err
	}
	if result.Matched == 0 {
		return fmt.Errorf("no unresolved error matching %s", fpPrefix)
	}
	fmt.Fprintf(w, "resolved %s\n", result.Fingerprint)
	return nil
}

func (c *CLI) runSilence(ctx context.Context, cmd *Command, w io.Writer) error {
	fp, expiresAt := cmd.reference, (*time.Time)(nil)
	if cmd.durationText != "" {
		t := time.Now().UTC().Add(cmd.duration)
		expiresAt = &t
	}
	result, err := c.Errors.Silence(ctx, fp, expiresAt, cmd.reason)
	if err != nil {
		return err
	}

	fp, expiresAt = result.Fingerprint, result.ExpiresAt
	if expiresAt != nil {
		fmt.Fprintf(w, "silenced %s until %s\n", fp, expiresAt.Format(time.RFC3339))
	} else {
		fmt.Fprintf(w, "silenced %s permanently\n", fp)
	}
	return nil
}

func (c *CLI) runSilences(ctx context.Context, cmd *Command, w io.Writer) error {

	entries, err := c.Errors.ListSilences(ctx)
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

func (c *CLI) runUnsilence(ctx context.Context, cmd *Command, w io.Writer) error {
	fp := cmd.reference
	fp, err := c.Errors.Unsilence(ctx, fp)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "unsilenced %s\n", fp)
	return nil
}
