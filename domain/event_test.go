package domain

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestTraceIDExtraction(t *testing.T) {
	// No contexts
	ev1 := Event{}
	if got := ev1.TraceID(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}

	// With trace context
	ev2 := Event{
		Contexts: map[string]json.RawMessage{
			"trace": json.RawMessage(`{"trace_id":"abc123","span_id":"def456"}`),
		},
	}
	if got := ev2.TraceID(); got != "abc123" {
		t.Fatalf("expected abc123, got %q", got)
	}

	// Invalid JSON in trace context
	ev3 := Event{
		Contexts: map[string]json.RawMessage{
			"trace": json.RawMessage(`not json`),
		},
	}
	if got := ev3.TraceID(); got != "" {
		t.Fatalf("expected empty for invalid json, got %q", got)
	}
}

func TestExceptionLevelDefault(t *testing.T) {
	ev := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{Type: "RuntimeError", Value: "boom"}},
		},
	}
	if got := ev.EffectiveLevel(); got != "error" {
		t.Fatalf("expected error, got %q", got)
	}
}

func TestMessageLevelDefault(t *testing.T) {
	ev := Event{Message: "hello"}
	if got := ev.EffectiveLevel(); got != "info" {
		t.Fatalf("expected info, got %q", got)
	}
}

func TestExplicitLevelOverride(t *testing.T) {
	ev := Event{Level: "fatal", Exception: &ExceptionData{
		Values: []ExceptionValue{{Type: "OOM", Value: "out of memory"}},
	}}
	if got := ev.EffectiveLevel(); got != "fatal" {
		t.Fatalf("expected fatal, got %q", got)
	}
}

func TestMessageText(t *testing.T) {
	// LogEntry.Formatted takes priority
	ev1 := Event{
		LogEntry: &LogEntry{Formatted: "formatted msg", Message: "template"},
		Message:  "plain",
	}
	if got := ev1.MessageText(); got != "formatted msg" {
		t.Fatalf("expected formatted msg, got %q", got)
	}

	// LogEntry.Message as fallback
	ev2 := Event{
		LogEntry: &LogEntry{Message: "template"},
		Message:  "plain",
	}
	if got := ev2.MessageText(); got != "template" {
		t.Fatalf("expected template, got %q", got)
	}

	// Plain message
	ev3 := Event{Message: "plain"}
	if got := ev3.MessageText(); got != "plain" {
		t.Fatalf("expected plain, got %q", got)
	}
}

func TestFingerprintGrouping(t *testing.T) {
	// Same exception type + location = same fingerprint
	event1 := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:  "IOError",
				Value: "file not found: a.txt",
				Stacktrace: &Stacktrace{
					Frames: []Frame{{Filename: "io.go", Function: "readFile", Lineno: 10}},
				},
			}},
		},
	}
	event2 := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:  "IOError",
				Value: "file not found: b.txt", // different message, same location
				Stacktrace: &Stacktrace{
					Frames: []Frame{{Filename: "io.go", Function: "readFile", Lineno: 10}},
				},
			}},
		},
	}

	fp1 := Fingerprint(&event1)
	fp2 := Fingerprint(&event2)
	if fp1 != fp2 {
		t.Fatalf("same type+location should have same fingerprint: %q != %q", fp1, fp2)
	}

	// Different location = different fingerprint
	event3 := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:  "IOError",
				Value: "file not found",
				Stacktrace: &Stacktrace{
					Frames: []Frame{{Filename: "io.go", Function: "writeFile", Lineno: 20}},
				},
			}},
		},
	}
	fp3 := Fingerprint(&event3)
	if fp1 == fp3 {
		t.Fatalf("different location should have different fingerprint")
	}
}

func TestSanitizeTruncatesLongMessage(t *testing.T) {
	msg := make([]byte, 12000)
	for i := range msg {
		msg[i] = 'x'
	}
	ev := Event{Message: string(msg)}
	ev.Sanitize()
	if len(ev.Message) != 10000 {
		t.Fatalf("expected message length 10000, got %d", len(ev.Message))
	}
}

func TestSanitizeTruncatesBreadcrumbs(t *testing.T) {
	vals := make([]json.RawMessage, 101)
	for i := range vals {
		vals[i] = json.RawMessage(`{"message":"crumb"}`)
	}
	ev := Event{Breadcrumbs: &BreadcrumbData{Values: vals}}
	ev.Sanitize()
	if len(ev.Breadcrumbs.Values) != 100 {
		t.Fatalf("expected 100 breadcrumbs, got %d", len(ev.Breadcrumbs.Values))
	}
	// Should keep the last 100 (most recent), so first original entry is dropped
}

func TestSanitizeTruncatesExceptionValues(t *testing.T) {
	vals := make([]ExceptionValue, 11)
	for i := range vals {
		vals[i] = ExceptionValue{Type: "Error", Value: "boom"}
	}
	ev := Event{Exception: &ExceptionData{Values: vals}}
	ev.Sanitize()
	if len(ev.Exception.Values) != 10 {
		t.Fatalf("expected 10 exception values, got %d", len(ev.Exception.Values))
	}
}

func TestSanitizeTruncatesStacktraceFrames(t *testing.T) {
	frames := make([]Frame, 101)
	for i := range frames {
		frames[i] = Frame{Filename: "app.go", Function: "main", Lineno: i}
	}
	ev := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:       "Error",
				Value:      "boom",
				Stacktrace: &Stacktrace{Frames: frames},
			}},
		},
	}
	ev.Sanitize()
	if len(ev.Exception.Values[0].Stacktrace.Frames) != 100 {
		t.Fatalf("expected 100 frames, got %d", len(ev.Exception.Values[0].Stacktrace.Frames))
	}
}

func TestSanitizeTruncatesTags(t *testing.T) {
	tags := make(map[string]string)
	for i := 0; i < 51; i++ {
		tags[fmt.Sprintf("key%03d", i)] = "value"
	}
	ev := Event{Tags: tags}
	ev.Sanitize()
	if len(ev.Tags) != 50 {
		t.Fatalf("expected 50 tags, got %d", len(ev.Tags))
	}
}

func TestSanitizeTruncatesTagValues(t *testing.T) {
	longVal := make([]byte, 300)
	for i := range longVal {
		longVal[i] = 'v'
	}
	ev := Event{Tags: map[string]string{"k": string(longVal)}}
	ev.Sanitize()
	if len(ev.Tags["k"]) != 200 {
		t.Fatalf("expected tag value length 200, got %d", len(ev.Tags["k"]))
	}
}

func TestSanitizeNormalizesInvalidLevel(t *testing.T) {
	ev := Event{Level: "critical", Message: "test"}
	ev.Sanitize()
	if ev.Level != "" {
		t.Fatalf("expected empty level for invalid input, got %q", ev.Level)
	}
	// EffectiveLevel should still return a sensible default
	if got := ev.EffectiveLevel(); got != "info" {
		t.Fatalf("expected EffectiveLevel info, got %q", got)
	}
}

func TestSanitizeNoOpForValidEvent(t *testing.T) {
	ev := Event{
		Message:     "something happened",
		Level:       "warning",
		Environment: "production",
		Release:     "1.0.0",
		ServerName:  "web-01",
		Platform:    "go",
		Tags:        map[string]string{"env": "prod"},
		Request:     &RequestData{URL: "https://example.com", Method: "GET"},
		Breadcrumbs: &BreadcrumbData{Values: []json.RawMessage{json.RawMessage(`{"msg":"hi"}`)}},
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:       "Error",
				Value:      "oops",
				Stacktrace: &Stacktrace{Frames: []Frame{{Filename: "a.go", Function: "f", Lineno: 1}}},
			}},
		},
	}
	// Snapshot values before sanitize
	msgBefore := ev.Message
	levelBefore := ev.Level
	envBefore := ev.Environment
	relBefore := ev.Release
	snBefore := ev.ServerName
	platBefore := ev.Platform
	tagsBefore := len(ev.Tags)
	bcBefore := len(ev.Breadcrumbs.Values)
	excBefore := len(ev.Exception.Values)
	framesBefore := len(ev.Exception.Values[0].Stacktrace.Frames)
	urlBefore := ev.Request.URL

	ev.Sanitize()

	if ev.Message != msgBefore {
		t.Fatal("message changed")
	}
	if ev.Level != levelBefore {
		t.Fatal("level changed")
	}
	if ev.Environment != envBefore {
		t.Fatal("environment changed")
	}
	if ev.Release != relBefore {
		t.Fatal("release changed")
	}
	if ev.ServerName != snBefore {
		t.Fatal("server_name changed")
	}
	if ev.Platform != platBefore {
		t.Fatal("platform changed")
	}
	if len(ev.Tags) != tagsBefore {
		t.Fatal("tags changed")
	}
	if len(ev.Breadcrumbs.Values) != bcBefore {
		t.Fatal("breadcrumbs changed")
	}
	if len(ev.Exception.Values) != excBefore {
		t.Fatal("exception values changed")
	}
	if len(ev.Exception.Values[0].Stacktrace.Frames) != framesBefore {
		t.Fatal("frames changed")
	}
	if ev.Request.URL != urlBefore {
		t.Fatal("request URL changed")
	}
}

func TestValidFingerprint(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"abc123", true},
		{"0123456789abcdef", true},  // 16 chars — max
		{"0123456789abcdef0", false}, // 17 chars — too long
		{"ABCDEF", false},           // uppercase not allowed
		{"xyz", false},              // non-hex
		{"abc 123", false},          // space
		{"a", true},                 // single char
	}
	for _, tc := range cases {
		got := ValidFingerprint(tc.input)
		if got != tc.want {
			t.Errorf("ValidFingerprint(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestMessageFingerprinting(t *testing.T) {
	// Same message = same fingerprint
	ev1 := Event{Message: "deploy started"}
	ev2 := Event{Message: "deploy started"}
	if Fingerprint(&ev1) != Fingerprint(&ev2) {
		t.Fatal("same message should produce same fingerprint")
	}

	// Different message = different fingerprint
	ev3 := Event{Message: "deploy finished"}
	if Fingerprint(&ev1) == Fingerprint(&ev3) {
		t.Fatal("different message should produce different fingerprint")
	}

	// Message vs exception = different fingerprint
	ev4 := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{Type: "Error", Value: "deploy started"}},
		},
	}
	if Fingerprint(&ev1) == Fingerprint(&ev4) {
		t.Fatal("message and exception with same text should differ")
	}
}

func TestDeriveState(t *testing.T) {
	// Resolved
	if got := DeriveState("2025-01-01T00:00:00Z", "2024-12-01T00:00:00Z"); got != "resolved" {
		t.Fatalf("expected resolved, got %q", got)
	}

	// New (first seen within the last hour)
	recent := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	if got := DeriveState("", recent); got != "new" {
		t.Fatalf("expected new, got %q", got)
	}

	// Ongoing (first seen more than an hour ago)
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if got := DeriveState("", old); got != "ongoing" {
		t.Fatalf("expected ongoing, got %q", got)
	}

	// Empty firstSeen with no resolvedAt
	if got := DeriveState("", ""); got != "ongoing" {
		t.Fatalf("expected ongoing for empty firstSeen, got %q", got)
	}

	// Invalid firstSeen format
	if got := DeriveState("", "not-a-date"); got != "ongoing" {
		t.Fatalf("expected ongoing for invalid firstSeen, got %q", got)
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
		err   bool
	}{
		{"24h", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"1h", time.Hour, false},
		{"", 0, true},
		{"x", 0, true},
		{"abc", 0, true},
		{"10x", 0, true},
	}

	for _, tt := range tests {
		got, err := ParseDuration(tt.input)
		if tt.err && err == nil {
			t.Errorf("ParseDuration(%q): expected error", tt.input)
		}
		if !tt.err && err != nil {
			t.Errorf("ParseDuration(%q): unexpected error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseTag(t *testing.T) {
	tests := []struct {
		input     string
		wantKey   string
		wantValue string
		wantOK    bool
	}{
		{"server=web-1", "server", "web-1", true},
		{"env=production", "env", "production", true},
		{"bad", "", "", false},
		{"=nokey", "", "", false},
		{"novalue=", "", "", false},
	}
	for _, tt := range tests {
		k, v, ok := ParseTag(tt.input)
		if ok != tt.wantOK || k != tt.wantKey || v != tt.wantValue {
			t.Errorf("ParseTag(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.input, k, v, ok, tt.wantKey, tt.wantValue, tt.wantOK)
		}
	}
}
