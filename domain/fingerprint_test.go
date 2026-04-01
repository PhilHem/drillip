package domain

import "testing"

func TestFingerprintMessageStable(t *testing.T) {
	// Two loguru-formatted messages with different timestamps but same error
	// must produce the same fingerprint.
	ev1 := &Event{
		Message: "2026-03-25 07:01:19.125 | ERROR    | entitlements_app.adapters.portal2:_login:258 - Portal2 role switch failed",
	}
	ev2 := &Event{
		Message: "2026-03-25 07:04:46.785 | ERROR    | entitlements_app.adapters.portal2:_login:258 - Portal2 role switch failed",
	}

	fp1 := Fingerprint(ev1)
	fp2 := Fingerprint(ev2)

	if fp1 != fp2 {
		t.Errorf("expected same fingerprint for identical errors with different timestamps\n  fp1=%s\n  fp2=%s", fp1, fp2)
	}
}

func TestFingerprintMessageDifferent(t *testing.T) {
	ev1 := &Event{Message: "Portal2 role switch failed"}
	ev2 := &Event{Message: "SAML Response not found"}

	if Fingerprint(ev1) == Fingerprint(ev2) {
		t.Error("different messages should produce different fingerprints")
	}
}

func TestFingerprintPrefersLogEntryMessage(t *testing.T) {
	// When logentry.message (template) is available, use it for fingerprinting
	// even if logentry.formatted differs.
	ev1 := &Event{
		LogEntry: &LogEntry{
			Message:   "Portal2 role switch failed for user %s",
			Formatted: "2026-03-25 07:01:00.000 | ERROR | mod:f:1 - Portal2 role switch failed for user alice",
		},
	}
	ev2 := &Event{
		LogEntry: &LogEntry{
			Message:   "Portal2 role switch failed for user %s",
			Formatted: "2026-03-25 07:02:00.000 | ERROR | mod:f:1 - Portal2 role switch failed for user bob",
		},
	}

	if Fingerprint(ev1) != Fingerprint(ev2) {
		t.Error("events with same logentry.message template should have same fingerprint")
	}
}

func TestFingerprintPlainMessageUnchanged(t *testing.T) {
	// Plain messages without log prefixes should still work.
	ev := &Event{Message: "Deployment started for v1.2.0"}
	fp := Fingerprint(ev)
	if fp == "" || len(fp) != 16 {
		t.Errorf("expected 16-char hex fingerprint, got %q", fp)
	}
}
