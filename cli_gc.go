package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	suffix := s[len(s)-1]
	numStr := s[:len(s)-1]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	switch suffix {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown suffix %q (use h/d/w)", string(suffix))
	}
}

func runGC(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: drillip gc <duration> (e.g., 7d, 30d, 24h)")
		return
	}

	dur, err := parseDuration(args[0])
	if err != nil {
		fmt.Fprintf(w, "%v\n", err)
		return
	}

	threshold := time.Now().UTC().Add(-dur).Format(time.RFC3339)

	res, err := db.Exec("DELETE FROM occurrences WHERE timestamp < ?", threshold)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	deleted, _ := res.RowsAffected()
	fmt.Fprintf(w, "deleted %d occurrences older than %s\n", deleted, args[0])
}
