package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

func runShow(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: error-sink show <fingerprint>")
		return
	}
	fp := args[0]

	var fullFP, typ, val, stacktrace, breadcrumbs, release, env, userCtx, tags, platform, firstSeen, lastSeen string
	var count int
	err := db.QueryRow(`
		SELECT fingerprint, type, value, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform,
			first_seen, last_seen, count
		FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1
	`, fp).Scan(&fullFP, &typ, &val, &stacktrace, &breadcrumbs,
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

	printHint(w, "error-sink trend "+fullFP[:8], "error-sink correlate "+fullFP[:8])
}
