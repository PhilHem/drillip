package main

import (
	"fmt"
	"io"
)

func runStats(_ []string, w io.Writer) {
	var uniqueCount, totalOccurrences int
	var minTime, maxTime string

	if err := db.QueryRow("SELECT COUNT(*) FROM errors").Scan(&uniqueCount); err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&totalOccurrences); err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}

	_ = db.QueryRow("SELECT MIN(first_seen) FROM errors").Scan(&minTime)
	_ = db.QueryRow("SELECT MAX(last_seen) FROM errors").Scan(&maxTime)

	fmt.Fprintf(w, "Unique errors:      %d\n", uniqueCount)
	fmt.Fprintf(w, "Total occurrences:  %d\n", totalOccurrences)
	if minTime != "" {
		fmt.Fprintf(w, "First seen:         %s\n", minTime)
	}
	if maxTime != "" {
		fmt.Fprintf(w, "Last seen:          %s\n", maxTime)
	}

	printHint(w, "error-sink top")
}
