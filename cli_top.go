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
	level := fs.String("level", "", "filter by level (error, warning, info, etc.)")
	_ = fs.Parse(args)

	query := `SELECT fingerprint, count, type, value, level, last_seen FROM errors`
	var queryArgs []interface{}
	if *level != "" {
		query += ` WHERE level = ?`
		queryArgs = append(queryArgs, *level)
	}
	query += ` ORDER BY count DESC LIMIT ?`
	queryArgs = append(queryArgs, *limit)

	rows, err := db.Query(query, queryArgs...)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	defer rows.Close()

	var tableRows [][]string
	for rows.Next() {
		var fp, typ, val, lvl, lastSeen string
		var count int
		if err := rows.Scan(&fp, &count, &typ, &val, &lvl, &lastSeen); err != nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339, lastSeen)
		tableRows = append(tableRows, []string{
			fp[:8], fmt.Sprintf("%d", count), lvl, typ, truncate(val, 50), timeAgo(t),
		})
	}

	if len(tableRows) == 0 {
		fmt.Fprintln(w, "no errors recorded")
		return
	}

	printTable(w, []string{"FINGERPRINT", "COUNT", "LEVEL", "TYPE", "VALUE", "LAST SEEN"}, tableRows)
	printHint(w, "drillip show <fingerprint>")
}
