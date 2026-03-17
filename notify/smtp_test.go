package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PhilHem/drillip/domain"
	"github.com/PhilHem/drillip/store"
)

// --- Resolved notification tests ---

func TestNotifyResolvedSendsEmail(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	var captured []byte
	n.sendMail = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		captured = msg
		return nil
	}

	resolved := []store.ResolvedError{
		{Fingerprint: "abcdef1234567890", Type: "ValueError", Value: "bad input", ResolvedAt: "2026-03-17T10:00:00Z"},
		{Fingerprint: "1234567890abcdef", Type: "IOError", Value: "connection refused", ResolvedAt: "2026-03-17T10:00:00Z"},
	}

	n.NotifyResolved(resolved)

	msg := string(captured)
	if !strings.Contains(msg, "Subject: [drillip] resolved: 2 errors in proj") {
		t.Errorf("expected resolved subject, got message:\n%s", msg[:min(len(msg), 400)])
	}
}

func TestNotifyResolvedEmptyListNoSend(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	calls := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls++
		return nil
	}

	n.NotifyResolved(nil)
	n.NotifyResolved([]store.ResolvedError{})

	if calls != 0 {
		t.Fatalf("expected 0 sends for empty resolved list, got %d", calls)
	}
}

func TestNotifyResolvedDisabledSMTP(t *testing.T) {
	n := NewNotifier(SMTPConfig{}, "proj", 0, 0, nil)
	calls := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls++
		return nil
	}

	n.NotifyResolved([]store.ResolvedError{
		{Fingerprint: "abcdef1234567890", Type: "ValueError", Value: "bad"},
	})

	if calls != 0 {
		t.Fatalf("expected 0 sends with disabled SMTP, got %d", calls)
	}
}

func TestFormatResolvedHTMLEmail(t *testing.T) {
	resolved := []store.ResolvedError{
		{Fingerprint: "abcdef1234567890", Type: "ValueError", Value: "bad input", ResolvedAt: "2026-03-17T10:00:00Z"},
		{Fingerprint: "1234567890abcdef", Type: "IOError", Value: "connection refused", ResolvedAt: "2026-03-17T10:00:00Z"},
	}

	body := formatResolvedHTMLEmail(resolved, "myproject")

	for _, want := range []string{
		"Resolved",
		"2 errors resolved",
		"#059669",           // green gradient
		"#047857",           // green gradient end
		"#f0fdf4",           // green row background
		"ValueError",
		"bad input",
		"abcdef12",
		"IOError",
		"connection refused",
		"12345678",
		"drillip top",
		"drillip recent",
		"myproject",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("resolved HTML missing %q", want)
		}
	}
}

func TestFormatResolvedPlainEmail(t *testing.T) {
	resolved := []store.ResolvedError{
		{Fingerprint: "abcdef1234567890", Type: "ValueError", Value: "bad input", ResolvedAt: "2026-03-17T10:00:00Z"},
		{Fingerprint: "1234567890abcdef", Type: "IOError", Value: "connection refused", ResolvedAt: "2026-03-17T10:00:00Z"},
	}

	text := formatResolvedPlainEmail(resolved, "myproject")

	for _, want := range []string{
		"RESOLVED: 2 errors in myproject",
		"1. ValueError: bad input (fp: abcdef12)",
		"2. IOError: connection refused (fp: 12345678)",
		"drillip top",
		"drillip recent",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("resolved plain text missing %q\ngot:\n%s", want, text)
		}
	}
}

func TestNotifyResolvedHTMLContainsErrorDetails(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	var captured []byte
	n.sendMail = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		captured = msg
		return nil
	}

	resolved := []store.ResolvedError{
		{Fingerprint: "abcdef1234567890", Type: "RuntimeError", Value: "crash", ResolvedAt: "2026-03-17T10:00:00Z"},
	}

	n.NotifyResolved(resolved)

	msg := string(captured)
	for _, want := range []string{"RuntimeError", "crash", "abcdef12"} {
		if !strings.Contains(msg, want) {
			t.Errorf("resolved email missing %q", want)
		}
	}
}

func TestSMTPConfigDisabledByDefault(t *testing.T) {
	cfg := SMTPConfig{}
	if cfg.Enabled() {
		t.Fatal("empty config should be disabled")
	}
}

func TestSMTPConfigEnabledWithHostAndTo(t *testing.T) {
	cfg := SMTPConfig{Host: "smtp.example.com", To: "dev@example.com"}
	if !cfg.Enabled() {
		t.Fatal("config with host and to should be enabled")
	}
}

func TestSMTPConfigNeedsTo(t *testing.T) {
	cfg := SMTPConfig{Host: "smtp.example.com"}
	if cfg.Enabled() {
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
		if got := cfg.Addr(); got != tt.want {
			t.Errorf("port=%q: got %q, want %q", tt.port, got, tt.want)
		}
	}
}

func TestFormatHTMLEmailException(t *testing.T) {
	ev := &domain.Event{
		EventID:     "abc-123-def",
		Level:       "error",
		Release:     "v1.2.0",
		Environment: "production",
		Platform:    "python",
		ServerName:  "hpc-entitlements",
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "ValueError",
				Value: "invalid input",
				Stacktrace: &domain.Stacktrace{
					Frames: []domain.Frame{
						{Filename: "views.py", Function: "get_data", Lineno: 10},
						{Filename: "app.py", Function: "validate", Lineno: 42},
					},
				},
			}},
		},
		Request: &domain.RequestData{URL: "https://example.com/api/", Method: "POST"},
		Tags:    map[string]string{"server": "web-1"},
		User:    json.RawMessage(`{"ip_address":"1.2.3.4","username":"alice"}`),
		Breadcrumbs: &domain.BreadcrumbData{
			Values: []json.RawMessage{
				json.RawMessage(`{"category":"http","message":"GET /api/","timestamp":"2026-03-16T14:23:04Z"}`),
			},
		},
	}

	body := formatHTMLEmail(ev, "abcdef1234567890", "entitlements", false, 0)

	for _, want := range []string{
		"New Issue",
		"ValueError",
		"invalid input",
		"abcdef1234567890",         // fingerprint
		"abc-123-def",              // event ID
		"entitlements",             // project
		"production",               // environment
		"v1.2.0",                   // release
		"hpc-entitlements",         // server
		"python",                   // platform
		"validate",                 // frame function
		"app.py",                   // frame file
		"get_data",                 // lower frame
		"https://example.com/api/", // request URL
		"POST",                     // request method
		"server",                   // tag key
		"web-1",                    // tag value
		"1.2.3.4",                  // user IP
		"alice",                    // username
		"GET /api/",                // breadcrumb
		"drillip show abcdef12",    // CLI hint
		"drillip correlate abcdef12",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("HTML body missing %q", want)
		}
	}
}

func TestFormatHTMLEmailMessage(t *testing.T) {
	ev := &domain.Event{
		Level:   "info",
		Message: "deploy started",
	}

	body := formatHTMLEmail(ev, "1234567890abcdef", "", false, 0)
	if !strings.Contains(body, "deploy started") {
		t.Fatal("missing message value")
	}
	if !strings.Contains(body, "info") {
		t.Fatal("missing level")
	}
	// No project in bar
	if strings.Contains(body, "&middot;") {
		t.Fatal("should not have project separator when project is empty")
	}
}

func TestFormatHTMLOmitsEmptySections(t *testing.T) {
	ev := &domain.Event{
		Level:   "error",
		Message: "simple error",
	}

	body := formatHTMLEmail(ev, "abcd1234abcd1234", "", false, 0)

	if strings.Contains(body, "Stacktrace") {
		t.Error("should not have Stacktrace section for message event")
	}
	if strings.Contains(body, "Request") {
		t.Error("should not have Request section without request data")
	}
	if strings.Contains(body, "User") {
		t.Error("should not have User section without user data")
	}
	if strings.Contains(body, "Breadcrumbs") {
		t.Error("should not have Breadcrumbs section without breadcrumbs")
	}
	if strings.Contains(body, "Tags") {
		t.Error("should not have Tags section without tags")
	}
}

func TestFormatPlainEmail(t *testing.T) {
	ev := &domain.Event{
		EventID:     "test-event-id",
		Level:       "error",
		Release:     "v1.0.0",
		Environment: "staging",
		Platform:    "python",
		ServerName:  "server-1",
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "RuntimeError",
				Value: "boom",
				Stacktrace: &domain.Stacktrace{
					Frames: []domain.Frame{
						{Filename: "a.py", Function: "inner", Lineno: 5},
						{Filename: "b.py", Function: "outer", Lineno: 10},
					},
				},
			}},
		},
		Request: &domain.RequestData{URL: "https://example.com/", Method: "GET"},
		Tags:    map[string]string{"env": "staging"},
	}

	body := formatPlainEmail(ev, "abcdef1234567890", "myproject", false, 0)

	for _, want := range []string{
		"Type:        RuntimeError",
		"Value:       boom",
		"Fingerprint: abcdef1234567890",
		"Event ID:    test-event-id",
		"Project: myproject",
		"outer (b.py:10)",
		"inner (a.py:5)",
		"Request: GET https://example.com/",
		"Environment: staging",
		"Release:     v1.0.0",
		"Server:      server-1",
		"env = staging",
		"drillip show abcdef12",
		"drillip correlate abcdef12",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plain text missing %q", want)
		}
	}
}

func TestBuildMultipartMIME(t *testing.T) {
	msg := buildMultipartMIME("from@test.com", "to@test.com", "Subject", "plain body", "<html>html body</html>")
	s := string(msg)

	if !strings.Contains(s, "multipart/alternative") {
		t.Error("missing multipart content type")
	}
	if !strings.Contains(s, "text/plain") {
		t.Error("missing plain text part")
	}
	if !strings.Contains(s, "text/html") {
		t.Error("missing HTML part")
	}
	if !strings.Contains(s, "plain body") {
		t.Error("missing plain text content")
	}
	if !strings.Contains(s, "<html>html body</html>") {
		t.Error("missing HTML content")
	}
}

func TestNotifyNoopWhenDisabled(t *testing.T) {
	ev := &domain.Event{Message: "test"}
	n := NewNotifier(SMTPConfig{}, "", 0, 0, nil)
	// Should not panic or send when SMTP is disabled
	n.NotifyNewError(ev, "abc123", false, 0)
}

func TestNewNotifierZeroCooldownSendsImmediately(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	calls := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls++
		return nil
	}
	ev := &domain.Event{Message: "test"}
	n.NotifyNewError(ev, "fp1", false, 0)
	n.NotifyNewError(ev, "fp1", false, 0)
	if calls != 2 {
		t.Fatalf("expected 2 sends with zero cooldown, got %d", calls)
	}
}

func TestSameFingerPrintThrottled(t *testing.T) {
	now := time.Now()
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 10*time.Second, 0, nil)
	n.now = func() time.Time { return now }
	calls := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls++
		return nil
	}
	ev := &domain.Event{Message: "test"}

	n.NotifyNewError(ev, "fp1", false, 0) // should send
	n.NotifyNewError(ev, "fp1", false, 0) // should be throttled (same fp, within cooldown)

	if calls != 1 {
		t.Fatalf("expected 1 send (second throttled), got %d", calls)
	}
}

func TestSameFingerPrintSendsAfterCooldown(t *testing.T) {
	now := time.Now()
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 10*time.Second, 0, nil)
	n.now = func() time.Time { return now }
	calls := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls++
		return nil
	}
	ev := &domain.Event{Message: "test"}

	n.NotifyNewError(ev, "fp1", false, 0) // should send

	// Advance time past cooldown
	now = now.Add(11 * time.Second)
	n.NotifyNewError(ev, "fp1", false, 0) // should send again

	if calls != 2 {
		t.Fatalf("expected 2 sends after cooldown expired, got %d", calls)
	}
}

func TestDifferentFingerprintsThrottledByGlobalCooldown(t *testing.T) {
	now := time.Now()
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 10*time.Second, 0, nil)
	n.now = func() time.Time { return now }
	calls := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls++
		return nil
	}
	ev := &domain.Event{Message: "test"}

	n.NotifyNewError(ev, "fp1", false, 0) // should send
	n.NotifyNewError(ev, "fp2", false, 0) // different fp, but global cooldown blocks

	if calls != 1 {
		t.Fatalf("expected 1 send (fp2 blocked by global cooldown), got %d", calls)
	}
}

func TestShouldNotifyPrunesOldEntries(t *testing.T) {
	now := time.Now()
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 10*time.Second, 0, nil)
	n.now = func() time.Time { return now }

	// Manually populate recent map with stale entries
	n.recent["stale1"] = now.Add(-20 * time.Second)
	n.recent["stale2"] = now.Add(-30 * time.Second)
	n.recent["fresh"] = now.Add(-5 * time.Second)

	n.mu.Lock()
	n.shouldNotify("newFp")
	n.mu.Unlock()

	if _, ok := n.recent["stale1"]; ok {
		t.Error("stale1 should have been pruned")
	}
	if _, ok := n.recent["stale2"]; ok {
		t.Error("stale2 should have been pruned")
	}
	if _, ok := n.recent["fresh"]; !ok {
		t.Error("fresh should not have been pruned")
	}
	if _, ok := n.recent["newFp"]; !ok {
		t.Error("newFp should be in the map")
	}
}

func TestRenderBreadcrumbsTruncatesLongMessages(t *testing.T) {
	long := strings.Repeat("x", 100)
	bd := &domain.BreadcrumbData{
		Values: []json.RawMessage{
			json.RawMessage(`{"category":"http","message":"` + long + `","timestamp":"2026-03-16T14:00:00Z"}`),
		},
	}
	html := renderBreadcrumbs(bd)
	if strings.Contains(html, long) {
		t.Error("should truncate long messages")
	}
	if !strings.Contains(html, "...") {
		t.Error("should have ellipsis for truncated message")
	}
}

func TestRenderBreadcrumbsLimitsFive(t *testing.T) {
	var vals []json.RawMessage
	for i := 0; i < 10; i++ {
		vals = append(vals, json.RawMessage(`{"category":"http","message":"msg","timestamp":"2026-03-16T14:00:00Z"}`))
	}
	bd := &domain.BreadcrumbData{Values: vals}
	html := renderBreadcrumbs(bd)
	count := strings.Count(html, ">msg<")
	if count != 5 {
		t.Errorf("expected 5 breadcrumbs, got %d", count)
	}
}

func TestRenderUserEmpty(t *testing.T) {
	if renderUser(nil) != "" {
		t.Error("nil should return empty")
	}
	if renderUser(json.RawMessage(`null`)) != "" {
		t.Error("null should return empty")
	}
	if renderUser(json.RawMessage(`{}`)) != "" {
		t.Error("empty object should return empty")
	}
}

func TestRenderUserFields(t *testing.T) {
	html := renderUser(json.RawMessage(`{"ip_address":"1.2.3.4","username":"bob"}`))
	if !strings.Contains(html, "1.2.3.4") {
		t.Error("missing IP")
	}
	if !strings.Contains(html, "bob") {
		t.Error("missing username")
	}
}

func TestExtractException(t *testing.T) {
	ev1 := &domain.Event{Exception: &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "FooError", Value: "bar"}}}}
	typ, val := extractException(ev1)
	if typ != "FooError" || val != "bar" {
		t.Errorf("got %q %q", typ, val)
	}

	ev2 := &domain.Event{Message: "hello"}
	typ, val = extractException(ev2)
	if typ != "message" || val != "hello" {
		t.Errorf("got %q %q", typ, val)
	}
}

func TestFormatHTMLEmailRegression(t *testing.T) {
	ev := &domain.Event{
		EventID: "reg-123",
		Level:   "error",
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "ValueError",
				Value: "bad input",
			}},
		},
	}

	body := formatHTMLEmail(ev, "abcdef1234567890", "myproject", true, 72*time.Hour)

	// Should have regression styling
	for _, want := range []string{
		"Regression",
		"#f59e0b",  // amber border/gradient color
		"#d97706",  // amber gradient end
		"#fffbeb",  // amber exception background
		"ValueError",
		"bad input",
		"resolved for 3 days",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("regression HTML missing %q", want)
		}
	}

	// Should NOT have new-issue styling
	if strings.Contains(body, "New Issue") {
		t.Error("regression HTML should not contain 'New Issue'")
	}
}

func TestFormatHTMLEmailNewIssueNoRegressionText(t *testing.T) {
	ev := &domain.Event{
		Level:   "error",
		Message: "something broke",
	}

	body := formatHTMLEmail(ev, "abcdef1234567890", "", false, 0)

	if strings.Contains(body, "Regression") {
		t.Error("non-regression HTML should not contain 'Regression'")
	}
	if strings.Contains(body, "resolved for") {
		t.Error("non-regression HTML should not contain 'resolved for'")
	}
	if !strings.Contains(body, "New Issue") {
		t.Error("non-regression HTML should contain 'New Issue'")
	}
}

func TestFormatPlainEmailRegression(t *testing.T) {
	ev := &domain.Event{
		Level:   "error",
		Message: "bad thing",
	}

	body := formatPlainEmail(ev, "abcdef1234567890", "proj", true, 48*time.Hour)

	if !strings.Contains(body, "STATUS: REGRESSION") {
		t.Error("regression plain text missing STATUS line")
	}
	if !strings.Contains(body, "resolved for 2 days") {
		t.Errorf("regression plain text missing duration; got:\n%s", body)
	}
}

func TestFormatPlainEmailNoRegression(t *testing.T) {
	ev := &domain.Event{
		Level:   "error",
		Message: "bad thing",
	}

	body := formatPlainEmail(ev, "abcdef1234567890", "proj", false, 0)

	if strings.Contains(body, "REGRESSION") {
		t.Error("non-regression plain text should not contain REGRESSION")
	}
}

func TestNotifyRegressionSubject(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	var captured []byte
	n.sendMail = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		captured = msg
		return nil
	}

	ev := &domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{Type: "TypeError", Value: "oops"}},
		},
	}

	n.NotifyNewError(ev, "fp1", true, 24*time.Hour)

	msg := string(captured)
	if !strings.Contains(msg, "Subject: [drillip] regression: TypeError") {
		t.Errorf("expected regression subject, got message:\n%s", msg[:min(len(msg), 300)])
	}
}

func TestNotifyNewErrorSubject(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	var captured []byte
	n.sendMail = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		captured = msg
		return nil
	}

	ev := &domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{Type: "TypeError", Value: "oops"}},
		},
	}

	n.NotifyNewError(ev, "fp1", false, 0)

	msg := string(captured)
	if !strings.Contains(msg, "Subject: [drillip] error: TypeError") {
		t.Errorf("expected normal subject, got message:\n%s", msg[:min(len(msg), 300)])
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30 seconds"},
		{5 * time.Minute, "5 minutes"},
		{1 * time.Hour, "1 hour"},
		{3 * time.Hour, "3 hours"},
		{24 * time.Hour, "1 day"},
		{72 * time.Hour, "3 days"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestSilencedFingerprintSkipsNotification(t *testing.T) {
	s, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, s)
	calls := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls++
		return nil
	}

	fp := "silenced12345678"
	if err := s.Silence(fp, nil, "test silence"); err != nil {
		t.Fatalf("silence: %v", err)
	}

	ev := &domain.Event{Message: "test"}
	n.NotifyNewError(ev, fp, false, 0)

	if calls != 0 {
		t.Fatalf("expected 0 sends for silenced fingerprint, got %d", calls)
	}
}

func TestNonSilencedFingerprintSendsNotification(t *testing.T) {
	s, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, s)
	calls := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls++
		return nil
	}

	ev := &domain.Event{Message: "test"}
	n.NotifyNewError(ev, "notsilenced12345", false, 0)

	if calls != 1 {
		t.Fatalf("expected 1 send for non-silenced fingerprint, got %d", calls)
	}
}

// --- Digest tests ---

func TestDigestZeroSendsImmediately(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	calls := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls++
		return nil
	}
	ev := &domain.Event{Message: "test"}
	n.NotifyNewError(ev, "fp1", false, 0)
	n.NotifyNewError(ev, "fp2", false, 0)
	if calls != 2 {
		t.Fatalf("expected 2 immediate sends with digest=0, got %d", calls)
	}
}

func TestDigestBatchesMultipleErrors(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 5*time.Minute, nil)
	calls := 0
	var captured []byte
	n.sendMail = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		calls++
		captured = msg
		return nil
	}

	ev1 := &domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{Type: "ValueError", Value: "bad input"}},
		},
	}
	ev2 := &domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{Type: "IOError", Value: "connection refused"}},
		},
	}
	ev3 := &domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{Type: "TimeoutError", Value: "request timeout"}},
		},
	}

	n.NotifyNewError(ev1, "fp100001abcdef00", false, 0)
	n.NotifyNewError(ev2, "fp200002abcdef00", false, 0)
	n.NotifyNewError(ev3, "fp300003abcdef00", true, 48*time.Hour)

	// No email sent yet — buffered
	if calls != 0 {
		t.Fatalf("expected 0 sends while buffered, got %d", calls)
	}

	// Manually flush
	n.flush()

	if calls != 1 {
		t.Fatalf("expected 1 digest send after flush, got %d", calls)
	}

	msg := string(captured)
	if !strings.Contains(msg, "Subject: [drillip] 3 new errors in proj") {
		t.Errorf("missing digest subject, got:\n%s", msg[:min(len(msg), 400)])
	}
}

func TestDigestSingleItemSendsIndividualEmail(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 5*time.Minute, nil)
	calls := 0
	var captured []byte
	n.sendMail = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		calls++
		captured = msg
		return nil
	}

	ev := &domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{Type: "ValueError", Value: "bad"}},
		},
	}

	n.NotifyNewError(ev, "fp100001abcdef00", false, 0)

	// Flush with single pending item -> should send individual email, not digest
	n.flush()

	if calls != 1 {
		t.Fatalf("expected 1 send, got %d", calls)
	}

	msg := string(captured)
	// Individual email subject, not digest
	if !strings.Contains(msg, "Subject: [drillip] error: ValueError") {
		t.Errorf("expected individual subject, got:\n%s", msg[:min(len(msg), 400)])
	}
	if strings.Contains(msg, "DIGEST") {
		t.Error("single item should not use digest format")
	}
}

func TestFlushClearsPending(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 5*time.Minute, nil)
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error { return nil }

	ev := &domain.Event{Message: "test"}
	n.NotifyNewError(ev, "fp1", false, 0)
	n.NotifyNewError(ev, "fp2", false, 0)

	n.flush()

	n.mu.Lock()
	pending := len(n.pending)
	n.mu.Unlock()

	if pending != 0 {
		t.Fatalf("expected 0 pending after flush, got %d", pending)
	}

	// Second flush should not send
	calls := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls++
		return nil
	}
	n.flush()
	if calls != 0 {
		t.Fatalf("expected 0 sends on second flush, got %d", calls)
	}
}

func TestCloseFlushes(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 5*time.Minute, nil)
	calls := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls++
		return nil
	}

	ev := &domain.Event{Message: "test"}
	n.NotifyNewError(ev, "fp1", false, 0)
	n.NotifyNewError(ev, "fp2", false, 0)

	n.Close()

	if calls != 1 {
		t.Fatalf("expected 1 digest send from Close, got %d", calls)
	}
}

func TestDigestHTMLContainsAllErrors(t *testing.T) {
	items := []pendingNotification{
		{
			Event: &domain.Event{
				Level: "error",
				Exception: &domain.ExceptionData{
					Values: []domain.ExceptionValue{{Type: "ValueError", Value: "invalid input"}},
				},
			},
			Fingerprint: "04827c09abcdef00",
			IsRegression: false,
		},
		{
			Event: &domain.Event{
				Level: "error",
				Exception: &domain.ExceptionData{
					Values: []domain.ExceptionValue{{Type: "IOError", Value: "connection refused"}},
				},
			},
			Fingerprint: "a3b1e7f2abcdef00",
			IsRegression: false,
		},
		{
			Event: &domain.Event{
				Level: "error",
				Exception: &domain.ExceptionData{
					Values: []domain.ExceptionValue{{Type: "TimeoutError", Value: "request timeout"}},
				},
			},
			Fingerprint: "9c4d5e6fabcdef00",
			IsRegression: true,
			ResolvedFor: 48 * time.Hour,
		},
	}

	html := formatDigestHTMLEmail(items, "myproject")

	for _, want := range []string{
		"Digest",
		"3 new errors",
		"#059669",          // green gradient
		"#047857",          // green gradient end
		"ValueError",
		"invalid input",
		"04827c09",
		"IOError",
		"connection refused",
		"a3b1e7f2",
		"TimeoutError",
		"request timeout",
		"9c4d5e6f",
		"regression",       // regression label
		"#fffbeb",          // amber background for regression row
		"drillip top",
		"drillip recent",
		"myproject",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("digest HTML missing %q", want)
		}
	}
}

func TestDigestPlainTextFormat(t *testing.T) {
	items := []pendingNotification{
		{
			Event: &domain.Event{
				Level: "error",
				Exception: &domain.ExceptionData{
					Values: []domain.ExceptionValue{{Type: "ValueError", Value: "invalid input"}},
				},
			},
			Fingerprint: "04827c09abcdef00",
			IsRegression: false,
		},
		{
			Event: &domain.Event{
				Level: "error",
				Exception: &domain.ExceptionData{
					Values: []domain.ExceptionValue{{Type: "IOError", Value: "connection refused"}},
				},
			},
			Fingerprint: "a3b1e7f2abcdef00",
			IsRegression: false,
		},
		{
			Event: &domain.Event{
				Level: "error",
				Exception: &domain.ExceptionData{
					Values: []domain.ExceptionValue{{Type: "TimeoutError", Value: "request timeout"}},
				},
			},
			Fingerprint: "9c4d5e6fabcdef00",
			IsRegression: true,
			ResolvedFor: 48 * time.Hour,
		},
	}

	text := formatDigestPlainEmail(items)

	for _, want := range []string{
		"DIGEST: 3 new errors",
		"1. [error] ValueError: invalid input (fp: 04827c09)",
		"2. [error] IOError: connection refused (fp: a3b1e7f2)",
		"3. [regression] TimeoutError: request timeout (fp: 9c4d5e6f, was resolved for 2 days)",
		"drillip top",
		"drillip recent",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("digest plain text missing %q\ngot:\n%s", want, text)
		}
	}
}

func TestSanitizeHeader(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"strips CR", "hello\rworld", "helloworld"},
		{"strips LF", "hello\nworld", "helloworld"},
		{"strips CRLF", "hello\r\nworld", "helloworld"},
		{"strips multiple", "a\rb\nc\r\nd", "abcd"},
		{"truncates to 200", strings.Repeat("x", 250), strings.Repeat("x", 200)},
		{"short string unchanged", "normal subject", "normal subject"},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeHeader(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeHeader(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSMTPHeaderInjection(t *testing.T) {
	// An attacker could craft an exception type containing CRLF sequences
	// to inject extra headers (e.g. Bcc) into the email.
	ev := &domain.Event{
		Exception: &domain.ExceptionData{
			Values: []domain.ExceptionValue{{
				Type:  "ValueError\r\nBcc: evil@attacker.com",
				Value: "injected",
			}},
		},
	}

	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	var captured []byte
	n.sendMail = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		captured = msg
		return nil
	}

	n.NotifyNewError(ev, "fp1", false, 0)

	msg := string(captured)

	// The CRLF must be stripped so the attacker's Bcc never becomes a
	// separate header line. Split on \r\n and verify no line starts with "Bcc:".
	headerEnd := strings.Index(msg, "\r\n\r\n")
	if headerEnd < 0 {
		t.Fatal("could not find end of MIME headers")
	}
	headers := msg[:headerEnd]

	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Errorf("SMTP header injection: found injected header line: %q", line)
		}
	}

	// The subject should still contain the sanitized type (without CRLF)
	if !strings.Contains(headers, "Subject: [drillip] error: ValueError") {
		t.Errorf("expected sanitized subject line, got headers:\n%s", headers)
	}

	// The CRLF characters themselves must be gone from the subject
	if strings.Contains(headers, "Subject: [drillip] error: ValueError\r\n") {
		t.Error("subject still contains raw CRLF")
	}
}

func TestNotifierConcurrentSafety(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	var mu sync.Mutex
	sendCount := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		mu.Lock()
		sendCount++
		mu.Unlock()
		return nil
	}

	const goroutines = 100
	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			ev := &domain.Event{
				Exception: &domain.ExceptionData{
					Values: []domain.ExceptionValue{{
						Type:  fmt.Sprintf("Error%d", i),
						Value: "concurrent test",
					}},
				},
			}
			n.NotifyNewError(ev, fmt.Sprintf("fp%d", i), false, 0)
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}

	mu.Lock()
	got := sendCount
	mu.Unlock()

	if got != goroutines {
		t.Fatalf("expected %d sends, got %d", goroutines, got)
	}
}

func TestDigestTimerFires(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 10*time.Millisecond, nil)
	calls := make(chan int, 1)
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		calls <- 1
		return nil
	}

	ev := &domain.Event{Message: "test"}
	n.NotifyNewError(ev, "fp1", false, 0)

	// Wait for timer to fire
	select {
	case <-calls:
		// ok
	case <-time.After(1 * time.Second):
		t.Fatal("digest timer did not fire within 1 second")
	}
}

func TestSendRetriesOnError(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	attempts := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		attempts++
		return errors.New("connection refused")
	}

	n.send("subject", "text", "<html>html</html>")

	if attempts != 3 {
		t.Fatalf("expected 3 retry attempts, got %d", attempts)
	}
}

func TestSendSucceedsOnSecondAttempt(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, 0, nil)
	attempts := 0
	n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		attempts++
		if attempts < 2 {
			return errors.New("temporary failure")
		}
		return nil
	}

	n.send("subject", "text", "<html>html</html>")

	if attempts != 2 {
		t.Fatalf("expected 2 attempts (fail then succeed), got %d", attempts)
	}
}

