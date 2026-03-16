package domain

import (
	"encoding/json"
	"testing"
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
