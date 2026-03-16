package main

import (
	"testing"
)

func TestSMTPConfigDisabledByDefault(t *testing.T) {
	cfg := SMTPConfig{}
	if cfg.enabled() {
		t.Fatal("empty config should be disabled")
	}
}

func TestSMTPConfigEnabledWithHostAndTo(t *testing.T) {
	cfg := SMTPConfig{Host: "smtp.example.com", To: "dev@example.com"}
	if !cfg.enabled() {
		t.Fatal("config with host and to should be enabled")
	}
}

func TestSMTPConfigNeedsTo(t *testing.T) {
	cfg := SMTPConfig{Host: "smtp.example.com"}
	if cfg.enabled() {
		t.Fatal("config without To should be disabled")
	}
}

func TestSMTPAddr(t *testing.T) {
	tests := []struct {
		port string
		want string
	}{
		{"", "smtp.example.com:25"},
		{"587", "smtp.example.com:587"},
	}
	for _, tt := range tests {
		cfg := SMTPConfig{Host: "smtp.example.com", Port: tt.port}
		if got := cfg.addr(); got != tt.want {
			t.Errorf("port=%q: got %q, want %q", tt.port, got, tt.want)
		}
	}
}

func TestFormatEmailException(t *testing.T) {
	ev := &Event{
		Level:       "error",
		Release:     "v1.2.0",
		Environment: "production",
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type:  "ValueError",
				Value: "invalid input",
				Stacktrace: &Stacktrace{
					Frames: []Frame{{
						Filename: "app.py",
						Function: "validate",
						Lineno:   42,
					}},
				},
			}},
		},
	}

	subject, body := formatEmail(ev, "abcdef1234567890")

	if subject != "[drillip] error: ValueError" {
		t.Fatalf("unexpected subject: %q", subject)
	}

	for _, want := range []string{
		"Type:        ValueError",
		"Value:       invalid input",
		"Fingerprint: abcdef1234567890",
		"Location:    app.py in validate, line 42",
		"Environment: production",
		"Release:     v1.2.0",
	} {
		if !contains(body, want) {
			t.Errorf("body missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestFormatEmailMessage(t *testing.T) {
	ev := &Event{
		Level:   "info",
		Message: "deploy started",
	}

	subject, body := formatEmail(ev, "1234567890abcdef")

	if subject != "[drillip] info: message" {
		t.Fatalf("unexpected subject: %q", subject)
	}
	if !contains(body, "Value:       deploy started") {
		t.Fatalf("body missing message value\nbody:\n%s", body)
	}
}

func TestBuildMIME(t *testing.T) {
	msg := buildMIME("from@test.com", "to@test.com", "Test Subject", "Test body")
	s := string(msg)

	for _, want := range []string{
		"From: from@test.com\r\n",
		"To: to@test.com\r\n",
		"Subject: Test Subject\r\n",
		"Content-Type: text/plain; charset=\"utf-8\"\r\n",
		"\r\n\r\nTest body",
	} {
		if !contains(s, want) {
			t.Errorf("MIME missing %q", want)
		}
	}
}

func TestNotifyNoopWhenDisabled(t *testing.T) {
	// Should not panic or error when SMTP is not configured
	ev := &Event{Message: "test"}
	notifyNewError(SMTPConfig{}, ev, "abc123")
}

func TestStoreEventReportsIsNew(t *testing.T) {
	setupTestDB(t)

	event := Event{
		Exception: &ExceptionData{
			Values: []ExceptionValue{{
				Type: "NewTestErr", Value: "first",
				Stacktrace: &Stacktrace{Frames: []Frame{{Filename: "n.go", Function: "f", Lineno: 1}}},
			}},
		},
	}

	result, err := storeEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if !result.IsNew {
		t.Fatal("first occurrence should be new")
	}

	result, err = storeEvent(&event)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if result.IsNew {
		t.Fatal("second occurrence should not be new")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
