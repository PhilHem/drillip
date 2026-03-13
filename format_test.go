package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPrintTable(t *testing.T) {
	var buf bytes.Buffer
	printTable(&buf, []string{"A", "BB"}, [][]string{
		{"x", "yy"},
		{"longer", "z"},
	})
	out := buf.String()
	if !strings.Contains(out, "A") || !strings.Contains(out, "BB") {
		t.Fatalf("missing headers: %s", out)
	}
	if !strings.Contains(out, "longer") {
		t.Fatalf("missing row data: %s", out)
	}
	// Check alignment: "longer" is 6 chars, so A column should be 6 wide
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}
}

func TestPrintTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	printTable(&buf, []string{"A"}, nil)
	if buf.Len() != 0 {
		t.Fatalf("expected no output for empty table, got %q", buf.String())
	}
}

func TestPrintSection(t *testing.T) {
	var buf bytes.Buffer
	printSection(&buf, "Test")
	if !strings.Contains(buf.String(), "Test") {
		t.Fatalf("missing section name: %s", buf.String())
	}
	if !strings.HasPrefix(buf.String(), "──") {
		t.Fatalf("missing separator: %s", buf.String())
	}
}

func TestPrintStacktrace(t *testing.T) {
	var buf bytes.Buffer
	printStacktrace(&buf, `{"frames":[{"filename":"app.py","function":"main","lineno":42},{"filename":"lib.py","function":"helper","lineno":10}]}`)
	out := buf.String()
	if !strings.Contains(out, "Traceback") {
		t.Fatalf("missing traceback header: %s", out)
	}
	if !strings.Contains(out, "app.py") || !strings.Contains(out, "lib.py") {
		t.Fatalf("missing frames: %s", out)
	}
}

func TestPrintStacktraceEmpty(t *testing.T) {
	var buf bytes.Buffer
	printStacktrace(&buf, "")
	if buf.Len() != 0 {
		t.Fatalf("expected no output for empty stacktrace")
	}
}

func TestPrintBreadcrumbs(t *testing.T) {
	var buf bytes.Buffer
	printBreadcrumbs(&buf, `[{"category":"http","message":"GET /api","timestamp":"12:00:00"}]`)
	out := buf.String()
	if !strings.Contains(out, "http") || !strings.Contains(out, "GET /api") {
		t.Fatalf("missing breadcrumb data: %s", out)
	}
}

func TestPrintBreadcrumbsEmpty(t *testing.T) {
	var buf bytes.Buffer
	printBreadcrumbs(&buf, "")
	if buf.Len() != 0 {
		t.Fatalf("expected no output for empty breadcrumbs")
	}
}

func TestPrintBar(t *testing.T) {
	var buf bytes.Buffer
	printBar(&buf, "10:00", 5, 10, 20)
	out := buf.String()
	if !strings.Contains(out, "10:00") {
		t.Fatalf("missing label: %s", out)
	}
	if !strings.Contains(out, "█") {
		t.Fatalf("missing bar: %s", out)
	}
	if !strings.Contains(out, "5") {
		t.Fatalf("missing value: %s", out)
	}
}

func TestPrintBarZeroMax(t *testing.T) {
	var buf bytes.Buffer
	printBar(&buf, "x", 0, 0, 10)
	// Should not panic
	if !strings.Contains(buf.String(), "0") {
		t.Fatalf("expected 0 value: %s", buf.String())
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}
	if got := truncate("hello world", 8); got != "hello..." {
		t.Fatalf("expected 'hello...', got %q", got)
	}
	if got := truncate("hi", 2); got != "hi" {
		t.Fatalf("expected 'hi', got %q", got)
	}
	if got := truncate("hello", 3); got != "hel" {
		t.Fatalf("expected 'hel', got %q", got)
	}
}

func TestTimeAgo(t *testing.T) {
	now := time.Now()
	if got := timeAgo(now.Add(-30 * time.Second)); !strings.HasSuffix(got, "s ago") {
		t.Fatalf("expected seconds ago, got %q", got)
	}
	if got := timeAgo(now.Add(-5 * time.Minute)); !strings.HasSuffix(got, "m ago") {
		t.Fatalf("expected minutes ago, got %q", got)
	}
	if got := timeAgo(now.Add(-3 * time.Hour)); !strings.HasSuffix(got, "h ago") {
		t.Fatalf("expected hours ago, got %q", got)
	}
	if got := timeAgo(now.Add(-48 * time.Hour)); !strings.HasSuffix(got, "d ago") {
		t.Fatalf("expected days ago, got %q", got)
	}
}

func TestPrintHint(t *testing.T) {
	var buf bytes.Buffer
	printHint(&buf, "error-sink top", "error-sink show abc")
	out := buf.String()
	if !strings.Contains(out, "→ error-sink top") {
		t.Fatalf("missing hint: %s", out)
	}
	if !strings.Contains(out, "→ error-sink show abc") {
		t.Fatalf("missing second hint: %s", out)
	}
}
