package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

func runShow(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip show <fingerprint>")
		return
	}
	fp := args[0]

	var fullFP, typ, val, lvl, stacktrace, breadcrumbs, release, env, userCtx, tags, platform, firstSeen, lastSeen string
	var count int
	err := db.QueryRow(`
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
	printTagDistribution(w, fullFP)

	printHint(w, "drillip trend "+fullFP[:8], "drillip correlate "+fullFP[:8],
		"drillip top --tag key=value")
}

// printTagDistribution shows how tag values are distributed across occurrences.
func printTagDistribution(w io.Writer, fp string) {
	rows, err := db.Query(`SELECT tags FROM occurrences WHERE fingerprint = ? AND tags != '' AND tags IS NOT NULL`, fp)
	if err != nil {
		return
	}
	defer rows.Close()

	// key → value → count
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
		for v, c := range values {
			sorted = append(sorted, kv{v, c})
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
