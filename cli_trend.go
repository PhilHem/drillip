package main

import (
	"fmt"
	"io"
	"time"
)

func runTrend(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip trend <fingerprint>")
		return
	}
	fp := args[0]

	// Resolve full fingerprint
	var fullFP string
	if err := db.QueryRow("SELECT fingerprint FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1", fp).Scan(&fullFP); err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	// Query occurrences grouped by hour for last 24h
	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	rows, err := db.Query(`
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
