package notify

import (
	"encoding/json"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/PhilHem/drillip/domain"
)

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
	n := NewNotifier(SMTPConfig{}, "", 0)
	// Should not panic or send when SMTP is disabled
	n.NotifyNewError(ev, "abc123", false, 0)
}

func TestNewNotifierZeroCooldownSendsImmediately(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0)
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
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 10*time.Second)
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
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 10*time.Second)
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
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 10*time.Second)
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
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 10*time.Second)
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
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0)
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
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0)
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
