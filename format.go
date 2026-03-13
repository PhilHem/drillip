package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

func printTable(w io.Writer, headers []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}

	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		fmt.Fprintf(w, "%-*s", widths[i], h)
	}
	fmt.Fprintln(w)

	// Print separator
	for i, width := range widths {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		fmt.Fprint(w, strings.Repeat("─", width))
	}
	fmt.Fprintln(w)

	// Print rows
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				fmt.Fprint(w, "  ")
			}
			if i < len(widths) {
				fmt.Fprintf(w, "%-*s", widths[i], cell)
			}
		}
		fmt.Fprintln(w)
	}
}

func printSection(w io.Writer, name string) {
	line := strings.Repeat("─", 40)
	fmt.Fprintf(w, "── %s %s\n", name, line[:max(1, 40-len(name)-1)])
}

func printHint(w io.Writer, hints ...string) {
	fmt.Fprintln(w)
	for _, h := range hints {
		fmt.Fprintf(w, "→ %s\n", h)
	}
}

func printStacktrace(w io.Writer, stackJSON string) {
	if stackJSON == "" {
		return
	}
	var st Stacktrace
	if json.Unmarshal([]byte(stackJSON), &st) != nil {
		return
	}
	if len(st.Frames) == 0 {
		return
	}

	fmt.Fprintln(w, "Traceback (most recent call last):")
	for _, f := range st.Frames {
		file := f.Filename
		if f.AbsPath != "" {
			file = f.AbsPath
		}
		fmt.Fprintf(w, "  File \"%s\", line %d, in %s\n", file, f.Lineno, f.Function)
	}
}

func printBreadcrumbs(w io.Writer, crumbsJSON string) {
	if crumbsJSON == "" {
		return
	}
	var crumbs []json.RawMessage
	if json.Unmarshal([]byte(crumbsJSON), &crumbs) != nil {
		return
	}
	if len(crumbs) == 0 {
		return
	}

	for _, raw := range crumbs {
		var c struct {
			Timestamp string `json:"timestamp"`
			Category  string `json:"category"`
			Message   string `json:"message"`
			Level     string `json:"level"`
		}
		if json.Unmarshal(raw, &c) != nil {
			continue
		}
		ts := c.Timestamp
		if ts == "" {
			ts = "          "
		}
		cat := c.Category
		if cat == "" {
			cat = "default"
		}
		fmt.Fprintf(w, "  %s  [%s] %s\n", ts, cat, c.Message)
	}
}

func printBar(w io.Writer, label string, value, maxValue, width int) {
	if maxValue == 0 {
		maxValue = 1
	}
	barLen := (value * width) / maxValue
	if barLen < 0 {
		barLen = 0
	}
	if barLen > width {
		barLen = width
	}
	bar := strings.Repeat("█", barLen) + strings.Repeat("░", width-barLen)
	fmt.Fprintf(w, "%s %s %d\n", label, bar, value)
}

// parseTag splits "key=value" into (key, value, true) or ("", "", false).
func parseTag(s string) (string, string, bool) {
	i := strings.IndexByte(s, '=')
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
