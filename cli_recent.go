package main

import (
	"flag"
	"fmt"
	"io"
	"time"
)

func runRecent(args []string, w io.Writer) {
	fs := flag.NewFlagSet("recent", flag.ExitOnError)
	hours := fs.Int("hours", 1, "look back N hours")
	level := fs.String("level", "", "filter by level (error, warning, info, etc.)")
	_ = fs.Parse(args)

	since := time.Now().UTC().Add(-time.Duration(*hours) * time.Hour).Format(time.RFC3339)

	query := `SELECT fingerprint, count, type, value, level, first_seen FROM errors WHERE first_seen > ?`
	queryArgs := []interface{}{since}
	if *level != "" {
		query += ` AND level = ?`
		queryArgs = append(queryArgs, *level)
	}
	query += ` ORDER BY first_seen DESC`

	rows, err := db.Query(query, queryArgs...)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	defer rows.Close()

	var tableRows [][]string
	for rows.Next() {
		var fp, typ, val, lvl, firstSeen string
		var count int
		if err := rows.Scan(&fp, &count, &typ, &val, &lvl, &firstSeen); err != nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339, firstSeen)
		tableRows = append(tableRows, []string{
			fp[:8], fmt.Sprintf("%d", count), lvl, typ, truncate(val, 50), timeAgo(t),
		})
	}

	if len(tableRows) == 0 {
		fmt.Fprintf(w, "no new errors in the last %d hour(s)\n", *hours)
		return
	}

	fmt.Fprintf(w, "New errors (last %dh):\n\n", *hours)
	printTable(w, []string{"FINGERPRINT", "COUNT", "LEVEL", "TYPE", "VALUE", "FIRST SEEN"}, tableRows)
	printHint(w, "drillip show <fingerprint>")
}
