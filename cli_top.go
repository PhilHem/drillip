package main

import (
	"flag"
	"fmt"
	"io"
	"time"
)

func runTop(args []string, w io.Writer) {
	fs := flag.NewFlagSet("top", flag.ExitOnError)
	limit := fs.Int("limit", 10, "number of errors to show")
	_ = fs.Parse(args)

	rows, err := db.Query(`
		SELECT fingerprint, count, type, value, last_seen
		FROM errors ORDER BY count DESC LIMIT ?
	`, *limit)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	defer rows.Close()

	var tableRows [][]string
	for rows.Next() {
		var fp, typ, val, lastSeen string
		var count int
		if err := rows.Scan(&fp, &count, &typ, &val, &lastSeen); err != nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339, lastSeen)
		tableRows = append(tableRows, []string{
			fp[:8], fmt.Sprintf("%d", count), typ, truncate(val, 50), timeAgo(t),
		})
	}

	if len(tableRows) == 0 {
		fmt.Fprintln(w, "no errors recorded")
		return
	}

	printTable(w, []string{"FINGERPRINT", "COUNT", "TYPE", "VALUE", "LAST SEEN"}, tableRows)
	printHint(w, "error-sink show <fingerprint>")
}
