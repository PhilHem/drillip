package main

import (
	"fmt"
	"io"
)

func runReleases(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: error-sink releases <fingerprint>")
		return
	}
	fp := args[0]

	// Resolve full fingerprint
	var fullFP string
	if err := db.QueryRow("SELECT fingerprint FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1", fp).Scan(&fullFP); err != nil {
		fmt.Fprintf(w, "error not found: %s\n", fp)
		return
	}

	rows, err := db.Query(`
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
