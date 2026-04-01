package notify

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/smtp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PhilHem/drillip/domain"
)

// SMTPConfig holds email notification settings.
// Notifications are disabled when Host or To is empty.
type SMTPConfig struct {
	Host       string // SMTP server hostname
	Port       string // SMTP server port (default "25")
	From       string // sender address
	To         string // recipient address
	User       string // optional SMTP username
	Pass       string // optional SMTP password
	SkipVerify bool   // skip TLS certificate verification
}

func (c SMTPConfig) Enabled() bool {
	return c.Host != "" && c.To != ""
}

func (c SMTPConfig) Addr() string {
	port := c.Port
	if port == "" {
		port = "25"
	}
	return net.JoinHostPort(c.Host, port)
}

// Notifier holds cooldown state and sends email notifications.
// It is safe for concurrent use from multiple goroutines.
type Notifier struct {
	SMTP       SMTPConfig
	Project    string
	Cooldown   time.Duration
	Digest     time.Duration  // batch window; 0 means send immediately
	onNotified func(string)   // called after send with fingerprint; nil = no-op

	mu       sync.Mutex
	lastSent time.Time
	recent   map[string]time.Time // fingerprint -> last notified
	pending  []pendingNotification
	timer    *time.Timer

	// now and sendMail are injectable for testing.
	now      func() time.Time
	sendMail func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// pendingNotification holds a buffered notification awaiting digest flush.
type pendingNotification struct {
	Event       *domain.Event
	Fingerprint string
	IsRegression bool
	ResolvedFor time.Duration
}

// NewNotifier creates a Notifier with the given SMTP config, project name,
// cooldown duration, digest window, and onNotified callback.
// A zero digest duration means notifications are sent immediately (no batching).
// The onNotified callback is called with the fingerprint after each successful send;
// pass nil to disable.
func NewNotifier(smtpCfg SMTPConfig, project string, cooldown, digest time.Duration, onNotified func(string)) *Notifier {
	return &Notifier{
		SMTP:       smtpCfg,
		Project:    project,
		Cooldown:   cooldown,
		Digest:     digest,
		onNotified: onNotified,
		recent:     make(map[string]time.Time),
	}
}

// SetSendMail overrides the function used to send emails. Intended for testing.
func (n *Notifier) SetSendMail(fn func(addr string, a smtp.Auth, from string, to []string, msg []byte) error) {
	n.sendMail = fn
}

// shouldNotify checks whether the notifier should send for the given fingerprint
// and updates internal state accordingly. It returns true if sending is allowed.
// Must be called with n.mu held.
func (n *Notifier) shouldNotify(fp string) bool {
	now := time.Now()
	if n.now != nil {
		now = n.now()
	}

	// Global cooldown
	if n.Cooldown > 0 && !n.lastSent.IsZero() && now.Sub(n.lastSent) < n.Cooldown {
		return false
	}

	// Per-fingerprint cooldown
	if n.Cooldown > 0 {
		if last, ok := n.recent[fp]; ok && now.Sub(last) < n.Cooldown {
			return false
		}
	}

	// Update state
	n.lastSent = now
	n.recent[fp] = now

	// Prune stale entries
	for k, t := range n.recent {
		if now.Sub(t) > n.Cooldown {
			delete(n.recent, k)
		}
	}

	return true
}

// NotifyNewError sends an email for a newly seen or regressed error, subject to cooldown.
// When regression is true, the email uses amber styling and includes resolvedFor duration.
// If digest batching is enabled, the notification is buffered and sent when the digest
// window expires. Safe to call from a goroutine.
func (n *Notifier) NotifyNewError(ev *domain.Event, fp string, regression bool, resolvedFor time.Duration) {
	if !n.SMTP.Enabled() {
		return
	}

	n.mu.Lock()
	ok := n.shouldNotify(fp)
	if !ok {
		n.mu.Unlock()
		slog.Info("notify: throttled notification", "fingerprint", fp)
		return
	}

	if n.Digest <= 0 {
		n.mu.Unlock()
		n.sendIndividual(ev, fp, regression, resolvedFor)
		return
	}

	// Digest mode: buffer the notification
	n.pending = append(n.pending, pendingNotification{
		Event:       ev,
		Fingerprint: fp,
		IsRegression: regression,
		ResolvedFor: resolvedFor,
	})
	if len(n.pending) == 1 {
		// First item — start digest timer
		n.timer = time.AfterFunc(n.Digest, n.flush)
	}
	n.mu.Unlock()
}

// sendIndividual sends a single notification email (non-digest path).
func (n *Notifier) sendIndividual(ev *domain.Event, fp string, regression bool, resolvedFor time.Duration) {
	evType, _ := extractException(ev)
	var subject, htmlBody, textBody string

	if regression {
		subject = sanitizeHeader(fmt.Sprintf("[drillip] regression: %s", evType))
		htmlBody = formatHTMLEmail(ev, fp, n.Project, true, resolvedFor)
		textBody = formatPlainEmail(ev, fp, n.Project, true, resolvedFor)
	} else {
		subject = sanitizeHeader(fmt.Sprintf("[drillip] %s: %s", ev.EffectiveLevel(), evType))
		htmlBody = formatHTMLEmail(ev, fp, n.Project, false, 0)
		textBody = formatPlainEmail(ev, fp, n.Project, false, 0)
	}

	n.send(subject, textBody, htmlBody)
	n.markNotified(fp)
}

// send transmits an email via SMTP with retry and exponential backoff.
func (n *Notifier) send(subject, textBody, htmlBody string) {
	msg := buildMultipartMIME(n.SMTP.From, n.SMTP.To, subject, textBody, htmlBody)

	var auth smtp.Auth
	if n.SMTP.User != "" {
		auth = smtp.PlainAuth("", n.SMTP.User, n.SMTP.Pass, n.SMTP.Host)
	}

	sendFn := n.sendMail
	if sendFn == nil {
		if n.SMTP.SkipVerify {
			sendFn = n.sendMailSkipVerify
		} else {
			sendFn = smtp.SendMail
		}
	}

	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second) // 2s, 4s backoff
		}
		err = sendFn(n.SMTP.Addr(), auth, n.SMTP.From, []string{n.SMTP.To}, msg)
		if err == nil {
			slog.Info("notify: email sent", "to", n.SMTP.To, "subject", subject)
			return
		}
		slog.Error("notify: send attempt failed", "attempt", attempt+1, "err", err)
	}
	slog.Error("notify: send failed after 3 attempts", "err", err)
}

// sendMailSkipVerify is like smtp.SendMail but skips TLS certificate verification.
// Used when the SMTP server uses a CA not in the container's trust store.
func (n *Notifier) sendMailSkipVerify(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	host, _, _ := net.SplitHostPort(addr)

	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()

	// STARTTLS with InsecureSkipVerify
	if err := c.StartTLS(&tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, //nolint:gosec // intentional, configured via DRILLIP_SMTP_SKIP_VERIFY
	}); err != nil {
		return err
	}

	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}

	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}

	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// SendTestEmail sends a simple test email to verify SMTP configuration.
// Returns nil on success, or the error if sending fails.
func (n *Notifier) SendTestEmail() error {
	if !n.SMTP.Enabled() {
		return fmt.Errorf("SMTP not configured")
	}

	subject := sanitizeHeader(fmt.Sprintf("[drillip] test email from %s", n.Project))
	textBody := fmt.Sprintf("This is a test email from drillip.\n\nProject: %s\nSMTP: %s\nTime: %s\n",
		n.Project, n.SMTP.Addr(), time.Now().UTC().Format(time.RFC3339))

	htmlBody := fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="color-scheme" content="light only"><meta name="supported-color-schemes" content="light only"></head>
<body style="margin:0;padding:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background-color:#f5f5f5;">
<table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="background-color:#f5f5f5;padding:20px 0;"><tr><td align="center">
<table role="presentation" width="600" cellspacing="0" cellpadding="0" style="background-color:#ffffff;border-radius:8px;box-shadow:0 2px 4px rgba(0,0,0,0.1);">
<tr><td style="background-color:#059669;padding:24px 40px;border-radius:8px 8px 0 0;">
<p style="margin:0;color:#ffffff;font-size:20px;font-weight:600;">SMTP Test Successful</p>
</td></tr>
<tr><td style="padding:32px 40px;">
<p style="margin:0 0 16px 0;color:#1e293b;font-size:15px;">Email delivery is working. Drillip will notify you when errors occur.</p>
<table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="background-color:#f8fafc;border-radius:6px;">
<tr><td style="padding:16px 20px;">
<p style="margin:0 0 8px 0;color:#64748b;font-size:13px;">Project: <strong style="color:#1e293b;">%s</strong></p>
<p style="margin:0 0 8px 0;color:#64748b;font-size:13px;">SMTP: <strong style="color:#1e293b;">%s</strong></p>
<p style="margin:0;color:#64748b;font-size:13px;">Time: <strong style="color:#1e293b;">%s</strong></p>
</td></tr></table>
</td></tr>
<tr><td style="padding:20px 40px;background-color:#f8fafc;border-top:1px solid #e2e8f0;border-radius:0 0 8px 8px;">
<p style="margin:0;color:#94a3b8;font-size:12px;">drillip error tracking</p>
</td></tr>
</table></td></tr></table></body></html>`,
		html.EscapeString(n.Project),
		html.EscapeString(n.SMTP.Addr()),
		time.Now().UTC().Format(time.RFC3339))

	msg := buildMultipartMIME(n.SMTP.From, n.SMTP.To, subject, textBody, htmlBody)

	var smtpAuth smtp.Auth
	if n.SMTP.User != "" {
		smtpAuth = smtp.PlainAuth("", n.SMTP.User, n.SMTP.Pass, n.SMTP.Host)
	}

	sendFn := n.sendMail
	if sendFn == nil {
		if n.SMTP.SkipVerify {
			sendFn = n.sendMailSkipVerify
		} else {
			sendFn = smtp.SendMail
		}
	}
	if err := sendFn(n.SMTP.Addr(), smtpAuth, n.SMTP.From, []string{n.SMTP.To}, msg); err != nil {
		return err
	}
	slog.Info("notify: test email sent", "to", n.SMTP.To)
	return nil
}

// flush sends all pending notifications as a digest (or individual if only one).
func (n *Notifier) flush() {
	n.mu.Lock()
	items := n.pending
	n.pending = nil
	if n.timer != nil {
		n.timer.Stop()
		n.timer = nil
	}
	n.mu.Unlock()

	if len(items) == 0 {
		return
	}

	if len(items) == 1 {
		p := items[0]
		n.sendIndividual(p.Event, p.Fingerprint, p.IsRegression, p.ResolvedFor)
		return
	}

	subject := sanitizeHeader(fmt.Sprintf("[drillip] %d new errors in %s", len(items), n.Project))
	htmlBody := formatDigestHTMLEmail(items, n.Project)
	textBody := formatDigestPlainEmail(items)
	n.send(subject, textBody, htmlBody)

	for _, p := range items {
		n.markNotified(p.Fingerprint)
	}
}

// Close stops any pending digest timer and flushes buffered notifications.
// Call this during graceful shutdown.
func (n *Notifier) Close() {
	n.mu.Lock()
	if n.timer != nil {
		n.timer.Stop()
		n.timer = nil
	}
	n.mu.Unlock()
	n.flush()
}

// markNotified records that a notification was sent for the given fingerprint,
// so the resolved email only includes errors the user actually heard about.
func (n *Notifier) markNotified(fp string) {
	if n.onNotified != nil {
		n.onNotified(fp)
	}
}

// NotifyResolved sends an email summarizing errors that were resolved.
// Safe to call from a goroutine.
func (n *Notifier) NotifyResolved(resolved []domain.ResolvedError) {
	if !n.SMTP.Enabled() || len(resolved) == 0 {
		return
	}

	subject := sanitizeHeader(fmt.Sprintf("[drillip] resolved: %d errors in %s", len(resolved), n.Project))
	htmlBody := formatResolvedHTMLEmail(resolved, n.Project)
	textBody := formatResolvedPlainEmail(resolved, n.Project)
	n.send(subject, textBody, htmlBody)
}

// --- Resolved email formats ---

func formatResolvedHTMLEmail(resolved []domain.ResolvedError, project string) string {
	var b strings.Builder

	// Compute summary stats
	totalOccurrences := 0
	for _, r := range resolved {
		totalOccurrences += r.Count
	}

	// Document start + outer table
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><meta name="color-scheme" content="light only"><meta name="supported-color-schemes" content="light only"></head>`)
	b.WriteString(`<body style="margin:0;padding:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;background-color:#f5f5f5;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#f5f5f5;padding:20px 0;"><tr><td align="center">`)
	b.WriteString(`<table role="presentation" width="600" cellspacing="0" cellpadding="0" style="background-color:#ffffff;border-radius:8px;box-shadow:0 2px 4px rgba(0,0,0,0.1);">`)

	// Project bar
	b.WriteString(`<tr><td style="background-color:#f1f5f9;padding:12px 40px;border-radius:8px 8px 0 0;border-bottom:1px solid #e2e8f0;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr>`)
	b.WriteString(`<td><span style="color:#1e293b;font-size:13px;font-weight:600;">drillip</span>`)
	if project != "" {
		b.WriteString(fmt.Sprintf(`<span style="color:#94a3b8;font-size:13px;">&nbsp;&middot;&nbsp;%s</span>`, html.EscapeString(project)))
	}
	b.WriteString(`</td></tr></table></td></tr>`)

	// Header — green with summary stats
	b.WriteString(`<tr><td style="background-color:#059669;padding:24px 40px;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr>`)
	b.WriteString(fmt.Sprintf(`<td><p style="margin:0;color:#d1fae5;font-size:13px;text-transform:uppercase;letter-spacing:0.5px;">Resolved</p><p style="margin:8px 0 0 0;color:#ffffff;font-size:20px;font-weight:600;">%d errors resolved</p>`, len(resolved)))
	if totalOccurrences > len(resolved) {
		b.WriteString(fmt.Sprintf(`<p style="margin:4px 0 0 0;color:#a7f3d0;font-size:13px;">%d total occurrences</p>`, totalOccurrences))
	}
	b.WriteString(`</td></tr></table></td></tr>`)

	// Error rows — compact badge style, one block per error.
	b.WriteString(`<tr><td style="padding:32px 40px;">`)

	for i, r := range resolved {
		fpShort := r.Fingerprint
		if len(fpShort) > 8 {
			fpShort = fpShort[:8]
		}
		evValue := domain.StripLogPrefix(r.Value)
		levelColor, levelBg := resolvedLevelStyle(r.Level)

		if i > 0 {
			b.WriteString(`<div style="height:8px;"></div>`)
		}

		b.WriteString(`<div style="border:1px solid #d1fae5;border-radius:6px;overflow:hidden;">`)

		// Main row: type + value + count badge + level badge
		b.WriteString(`<div style="padding:12px 16px;background-color:#f0fdf4;">`)
		b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr>`)
		b.WriteString(fmt.Sprintf(`<td style="width:1%%;white-space:nowrap;padding-right:10px;vertical-align:top;"><span style="color:#1e293b;font-size:13px;font-weight:600;font-family:'SF Mono',Monaco,'Courier New',monospace;">%s</span></td>`, html.EscapeString(r.Type)))
		b.WriteString(fmt.Sprintf(`<td style="color:#475569;font-size:13px;line-height:1.4;vertical-align:top;word-break:break-word;">%s</td>`, html.EscapeString(evValue)))
		b.WriteString(`<td style="width:1%%;white-space:nowrap;text-align:right;vertical-align:top;padding-left:10px;">`)
		if r.Count > 1 {
			b.WriteString(fmt.Sprintf(`<span style="display:inline-block;padding:2px 6px;border-radius:10px;background-color:#e2e8f0;color:#475569;font-size:11px;font-weight:500;margin-right:4px;">%d&times;</span>`, r.Count))
		}
		b.WriteString(fmt.Sprintf(`<span style="display:inline-block;padding:2px 8px;border-radius:10px;background-color:%s;color:%s;font-size:11px;font-weight:500;">%s</span>`, levelBg, levelColor, html.EscapeString(r.Level)))
		b.WriteString(`</td></tr></table></div>`)

		// Footer: fingerprint + time span
		timeSpan := resolvedTimeSpan(r.FirstSeen, r.LastSeen)
		b.WriteString(`<div style="padding:6px 16px;background-color:#f8fafc;border-top:1px solid #e2e8f0;">`)
		b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr>`)
		b.WriteString(fmt.Sprintf(`<td style="color:#94a3b8;font-size:11px;font-family:'SF Mono',Monaco,'Courier New',monospace;">%s</td>`, html.EscapeString(fpShort)))
		if timeSpan != "" {
			b.WriteString(fmt.Sprintf(`<td style="color:#94a3b8;font-size:11px;text-align:right;">%s</td>`, timeSpan))
		}
		b.WriteString(`</tr></table></div>`)

		b.WriteString(`</div>`)
	}

	b.WriteString(`</td></tr>`)

	// CLI hint
	b.WriteString(`<tr><td style="padding:0 40px 32px 40px;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#f0f9ff;border:1px solid #bae6fd;border-radius:6px;">`)
	b.WriteString(`<tr><td style="padding:16px 20px;"><p style="margin:0 0 8px 0;color:#0369a1;font-size:13px;font-weight:600;">View status</p><p style="margin:0 0 4px 0;color:#1e293b;font-size:13px;font-family:'SF Mono',Monaco,'Courier New',monospace;">drillip top</p><p style="margin:0;color:#1e293b;font-size:13px;font-family:'SF Mono',Monaco,'Courier New',monospace;">drillip recent</p></td></tr>`)
	b.WriteString(`</table></td></tr>`)

	// Footer
	b.WriteString(`<tr><td style="padding:20px 40px;background-color:#f8fafc;border-top:1px solid #e2e8f0;border-radius:0 0 8px 8px;"><table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr><td style="color:#94a3b8;font-size:12px;">drillip error tracking</td></tr></table></td></tr>`)

	// Close tables
	b.WriteString(`</table></td></tr></table></body></html>`)

	return b.String()
}

// resolvedLevelStyle returns (text color, background color) for level badges.
func resolvedLevelStyle(level string) (string, string) {
	switch level {
	case "fatal":
		return "#991b1b", "#fef2f2"
	case "error":
		return "#b91c1c", "#fef2f2"
	case "warning":
		return "#92400e", "#fffbeb"
	case "info":
		return "#0c4a6e", "#f0f9ff"
	case "debug":
		return "#64748b", "#f8fafc"
	default:
		return "#64748b", "#f1f5f9"
	}
}

// resolvedTimeSpan returns a human-readable time span like "Mar 13 – Mar 25".
func resolvedTimeSpan(firstSeen, lastSeen string) string {
	f, errF := time.Parse(time.RFC3339, firstSeen)
	l, errL := time.Parse(time.RFC3339, lastSeen)
	if errF != nil || errL != nil {
		return ""
	}
	if f.Format("2006-01-02") == l.Format("2006-01-02") {
		return f.Format("Jan 2")
	}
	return f.Format("Jan 2") + " &ndash; " + l.Format("Jan 2")
}

func formatResolvedPlainEmail(resolved []domain.ResolvedError, project string) string {
	var b strings.Builder

	totalOccurrences := 0
	for _, r := range resolved {
		totalOccurrences += r.Count
	}

	b.WriteString(fmt.Sprintf("RESOLVED: %d errors in %s", len(resolved), project))
	if totalOccurrences > len(resolved) {
		b.WriteString(fmt.Sprintf(" (%d total occurrences)", totalOccurrences))
	}
	b.WriteString("\n\n")

	for i, r := range resolved {
		fpShort := r.Fingerprint
		if len(fpShort) > 8 {
			fpShort = fpShort[:8]
		}
		evValue := domain.StripLogPrefix(r.Value)

		b.WriteString(fmt.Sprintf("%d. [%s] %s: %s\n", i+1, r.Level, r.Type, evValue))
		b.WriteString(fmt.Sprintf("   fp: %s", fpShort))
		if r.Count > 1 {
			b.WriteString(fmt.Sprintf("  |  %dx", r.Count))
		}
		if r.FirstSeen != "" {
			b.WriteString(fmt.Sprintf("  |  %s", resolvedTimeSpanPlain(r.FirstSeen, r.LastSeen)))
		}
		b.WriteString("\n\n")
	}

	b.WriteString("---\nView status:\n  drillip top\n  drillip recent\n")

	return b.String()
}

func resolvedTimeSpanPlain(firstSeen, lastSeen string) string {
	f, errF := time.Parse(time.RFC3339, firstSeen)
	l, errL := time.Parse(time.RFC3339, lastSeen)
	if errF != nil || errL != nil {
		return ""
	}
	if f.Format("2006-01-02") == l.Format("2006-01-02") {
		return f.Format("Jan 2")
	}
	return f.Format("Jan 2") + " – " + l.Format("Jan 2")
}

// --- Digest email formats ---

func formatDigestHTMLEmail(items []pendingNotification, project string) string {
	var b strings.Builder

	// Document start + outer table
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><meta name="color-scheme" content="light only"><meta name="supported-color-schemes" content="light only"></head>`)
	b.WriteString(`<body style="margin:0;padding:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;background-color:#f5f5f5;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#f5f5f5;padding:20px 0;"><tr><td align="center">`)
	b.WriteString(`<table role="presentation" width="600" cellspacing="0" cellpadding="0" style="background-color:#ffffff;border-radius:8px;box-shadow:0 2px 4px rgba(0,0,0,0.1);">`)

	// Project bar
	b.WriteString(`<tr><td style="background-color:#f1f5f9;padding:12px 40px;border-radius:8px 8px 0 0;border-bottom:1px solid #e2e8f0;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr>`)
	b.WriteString(`<td><span style="color:#1e293b;font-size:13px;font-weight:600;">drillip</span>`)
	if project != "" {
		b.WriteString(fmt.Sprintf(`<span style="color:#94a3b8;font-size:13px;">&nbsp;&middot;&nbsp;%s</span>`, html.EscapeString(project)))
	}
	b.WriteString(`</td></tr></table></td></tr>`)

	// Header — green for digest
	b.WriteString(`<tr><td style="background-color:#059669;padding:24px 40px;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr>`)
	b.WriteString(fmt.Sprintf(`<td><p style="margin:0;color:#d1fae5;font-size:13px;text-transform:uppercase;letter-spacing:0.5px;">Digest</p><p style="margin:8px 0 0 0;color:#ffffff;font-size:20px;font-weight:600;">%d new errors</p></td>`, len(items)))
	b.WriteString(`</tr></table></td></tr>`)

	// Error table
	b.WriteString(`<tr><td style="padding:32px 40px;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="border:1px solid #e2e8f0;border-radius:6px;overflow:hidden;">`)

	// Table header
	b.WriteString(`<tr style="background-color:#f8fafc;">`)
	b.WriteString(`<td style="padding:8px 12px;color:#64748b;font-size:12px;font-weight:600;">Level</td>`)
	b.WriteString(`<td style="padding:8px 12px;color:#64748b;font-size:12px;font-weight:600;">Type</td>`)
	b.WriteString(`<td style="padding:8px 12px;color:#64748b;font-size:12px;font-weight:600;">Value</td>`)
	b.WriteString(`<td style="padding:8px 12px;color:#64748b;font-size:12px;font-weight:600;">Fingerprint</td>`)
	b.WriteString(`</tr>`)

	// One row per error
	for _, p := range items {
		evType, evValue := extractException(p.Event)
		level := p.Event.EffectiveLevel()
		fpShort := p.Fingerprint
		if len(fpShort) > 8 {
			fpShort = fpShort[:8]
		}

		// Truncate long values
		if len(evValue) > 50 {
			evValue = evValue[:47] + "..."
		}

		// Row background: amber for regressions, white otherwise
		rowBg := "#ffffff"
		if p.IsRegression {
			rowBg = "#fffbeb"
		}

		// Level badge colors
		badgeBg, badgeColor := "#fee2e2", "#991b1b" // error default
		if level == "warning" {
			badgeBg, badgeColor = "#fef3c7", "#92400e"
		} else if level == "info" {
			badgeBg, badgeColor = "#dbeafe", "#1e40af"
		}
		if p.IsRegression {
			badgeBg, badgeColor = "#fef3c7", "#92400e"
		}

		levelLabel := level
		if p.IsRegression {
			levelLabel = "regression"
		}

		b.WriteString(fmt.Sprintf(`<tr style="background-color:%s;">`, rowBg))
		b.WriteString(fmt.Sprintf(`<td style="padding:10px 12px;border-top:1px solid #e2e8f0;"><span style="display:inline-block;padding:2px 8px;background-color:%s;color:%s;font-size:11px;border-radius:3px;font-weight:500;">%s</span></td>`, badgeBg, badgeColor, html.EscapeString(levelLabel)))
		b.WriteString(fmt.Sprintf(`<td style="padding:10px 12px;border-top:1px solid #e2e8f0;color:#1e293b;font-size:13px;font-weight:600;font-family:'SF Mono',Monaco,'Courier New',monospace;">%s</td>`, html.EscapeString(evType)))
		b.WriteString(fmt.Sprintf(`<td style="padding:10px 12px;border-top:1px solid #e2e8f0;color:#475569;font-size:13px;">%s</td>`, html.EscapeString(evValue)))
		b.WriteString(fmt.Sprintf(`<td style="padding:10px 12px;border-top:1px solid #e2e8f0;font-family:'SF Mono',Monaco,'Courier New',monospace;font-size:12px;color:#64748b;">%s</td>`, html.EscapeString(fpShort)))
		b.WriteString(`</tr>`)
	}

	b.WriteString(`</table></td></tr>`)

	// CLI hint
	b.WriteString(`<tr><td style="padding:0 40px 32px 40px;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#f0f9ff;border:1px solid #bae6fd;border-radius:6px;">`)
	b.WriteString(`<tr><td style="padding:16px 20px;"><p style="margin:0 0 8px 0;color:#0369a1;font-size:13px;font-weight:600;">Investigate</p><p style="margin:0 0 4px 0;color:#1e293b;font-size:13px;font-family:'SF Mono',Monaco,'Courier New',monospace;">drillip top</p><p style="margin:0;color:#1e293b;font-size:13px;font-family:'SF Mono',Monaco,'Courier New',monospace;">drillip recent</p></td></tr>`)
	b.WriteString(`</table></td></tr>`)

	// Footer
	b.WriteString(`<tr><td style="padding:20px 40px;background-color:#f8fafc;border-top:1px solid #e2e8f0;border-radius:0 0 8px 8px;"><table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr><td style="color:#94a3b8;font-size:12px;">drillip error tracking</td></tr></table></td></tr>`)

	// Close tables
	b.WriteString(`</table></td></tr></table></body></html>`)

	return b.String()
}

func formatDigestPlainEmail(items []pendingNotification) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("DIGEST: %d new errors\n\n", len(items)))

	for i, p := range items {
		evType, evValue := extractException(p.Event)
		level := p.Event.EffectiveLevel()
		fpShort := p.Fingerprint
		if len(fpShort) > 8 {
			fpShort = fpShort[:8]
		}

		if p.IsRegression {
			line := fmt.Sprintf("%d. [regression] %s: %s (fp: %s", i+1, evType, evValue, fpShort)
			if p.ResolvedFor > 0 {
				line += fmt.Sprintf(", was resolved for %s", formatDuration(p.ResolvedFor))
			}
			line += ")\n"
			b.WriteString(line)
		} else {
			b.WriteString(fmt.Sprintf("%d. [%s] %s: %s (fp: %s)\n", i+1, level, evType, evValue, fpShort))
		}
	}

	b.WriteString("\n---\nInvestigate:\n  drillip top\n  drillip recent\n")

	return b.String()
}

func extractException(ev *domain.Event) (evType, evValue string) {
	if ev.Exception != nil && len(ev.Exception.Values) > 0 {
		return ev.Exception.Values[0].Type, ev.Exception.Values[0].Value
	}
	return "message", ev.MessageText()
}

// --- HTML email ---

func formatHTMLEmail(ev *domain.Event, fp, project string, isRegression bool, resolvedFor time.Duration) string {
	evType, evValue := extractException(ev)
	level := ev.EffectiveLevel()
	now := time.Now().UTC().Format("January 2, 2006, 3:04:05 p.m. UTC")
	fpShort := fp
	if len(fpShort) > 8 {
		fpShort = fpShort[:8]
	}

	// Choose colors based on regression vs new
	headerBg := "#e74c3c"
	headerLabel := "New Issue"
	headerLabelColor := "#fecaca"
	exceptionBg := "#fef2f2"
	exceptionBorder := "#e74c3c"
	levelBadgeBg := "rgba(255,255,255,0.2)"
	if isRegression {
		headerBg = "#d97706"
		headerLabel = "Regression"
		headerLabelColor = "#fef3c7"
		exceptionBg = "#fffbeb"
		exceptionBorder = "#f59e0b"
		levelBadgeBg = "rgba(255,255,255,0.2)"
	}

	var b strings.Builder

	// Document start + outer table
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><meta name="color-scheme" content="light only"><meta name="supported-color-schemes" content="light only"></head>`)
	b.WriteString(`<body style="margin:0;padding:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;background-color:#f5f5f5;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#f5f5f5;padding:20px 0;"><tr><td align="center">`)
	b.WriteString(`<table role="presentation" width="600" cellspacing="0" cellpadding="0" style="background-color:#ffffff;border-radius:8px;box-shadow:0 2px 4px rgba(0,0,0,0.1);">`)

	// Project bar
	b.WriteString(`<tr><td style="background-color:#f1f5f9;padding:12px 40px;border-radius:8px 8px 0 0;border-bottom:1px solid #e2e8f0;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr>`)
	b.WriteString(`<td><span style="color:#1e293b;font-size:13px;font-weight:600;">drillip</span>`)
	if project != "" {
		b.WriteString(fmt.Sprintf(`<span style="color:#94a3b8;font-size:13px;">&nbsp;&middot;&nbsp;%s</span>`, html.EscapeString(project)))
	}
	b.WriteString(`</td>`)
	if ev.EventID != "" {
		b.WriteString(fmt.Sprintf(`<td align="right"><span style="color:#94a3b8;font-size:12px;font-family:'SF Mono',Monaco,'Courier New',monospace;">%s</span></td>`, html.EscapeString(ev.EventID)))
	}
	b.WriteString(`</tr></table></td></tr>`)

	// Header
	b.WriteString(fmt.Sprintf(`<tr><td style="background-color:%s;padding:24px 40px;">`, headerBg))
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr>`)
	b.WriteString(fmt.Sprintf(`<td><p style="margin:0;color:%s;font-size:13px;text-transform:uppercase;letter-spacing:0.5px;">%s</p><p style="margin:8px 0 0 0;color:#ffffff;font-size:20px;font-weight:600;">%s</p></td>`, headerLabelColor, headerLabel, html.EscapeString(evType)))
	b.WriteString(fmt.Sprintf(`<td align="right" style="vertical-align:top;"><span style="display:inline-block;padding:4px 12px;border-radius:12px;background-color:%s;color:#fff;font-size:12px;font-weight:500;">%s</span></td>`, levelBadgeBg, html.EscapeString(level)))
	b.WriteString(`</tr></table></td></tr>`)

	// Content start
	b.WriteString(`<tr><td style="padding:32px 40px;">`)

	// Exception
	b.WriteString(sectionLabel("Exception"))
	b.WriteString(fmt.Sprintf(`<table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="margin-bottom:28px;"><tr><td style="padding:16px 20px;background-color:%s;border-left:3px solid %s;border-radius:0 6px 6px 0;"><p style="margin:0;color:#1e293b;font-size:15px;font-weight:600;font-family:'SF Mono',Monaco,'Courier New',monospace;">%s</p><p style="margin:6px 0 0 0;color:#475569;font-size:14px;line-height:1.5;">%s</p>`, exceptionBg, exceptionBorder, html.EscapeString(evType), html.EscapeString(evValue)))

	// Regression resolved-for line
	if isRegression && resolvedFor > 0 {
		b.WriteString(fmt.Sprintf(`<p style="margin:8px 0 0 0;color:#92400e;font-size:13px;font-style:italic;">This error was resolved for %s before reappearing.</p>`, formatDuration(resolvedFor)))
	}

	b.WriteString(`</td></tr></table>`)

	// Stacktrace
	if ev.Exception != nil && len(ev.Exception.Values) > 0 {
		exc := ev.Exception.Values[0]
		if exc.Stacktrace != nil && len(exc.Stacktrace.Frames) > 0 {
			b.WriteString(sectionLabel("Stacktrace"))
			b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="margin-bottom:28px;"><tr><td>`)
			b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#1e293b;border-radius:6px;">`)
			// Reverse iterate (top frame = last in Sentry's array)
			frames := exc.Stacktrace.Frames
			for i := len(frames) - 1; i >= 0; i-- {
				f := frames[i]
				border := ` border-bottom:1px solid #334155;`
				if i == 0 {
					border = ""
				}
				b.WriteString(fmt.Sprintf(`<tr><td style="padding:14px 20px;%s"><p style="margin:0 0 4px 0;color:#e2e8f0;font-size:14px;font-family:'SF Mono',Monaco,'Courier New',monospace;">%s</p><p style="margin:0;color:#94a3b8;font-size:12px;font-family:'SF Mono',Monaco,'Courier New',monospace;">%s:<span style="color:#64748b;">%d</span></p></td></tr>`,
					border, html.EscapeString(f.Function), html.EscapeString(f.Filename), f.Lineno))
			}
			b.WriteString(`</table></td></tr></table>`)
		}
	}

	// Request
	if ev.Request != nil && ev.Request.URL != "" {
		b.WriteString(sectionLabel("Request"))
		b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="margin-bottom:28px;"><tr><td style="padding:16px 20px;background-color:#f8fafc;border-radius:6px;">`)
		b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0">`)
		b.WriteString(fmt.Sprintf(`<tr><td><p style="margin:0 0 4px 0;color:#64748b;font-size:12px;text-transform:uppercase;letter-spacing:0.5px;">URL</p><p style="margin:0;color:#1e293b;font-size:14px;font-family:'SF Mono',Monaco,'Courier New',monospace;word-break:break-all;">%s</p></td></tr>`, html.EscapeString(ev.Request.URL)))
		if ev.Request.Method != "" {
			b.WriteString(fmt.Sprintf(`<tr><td style="padding-top:12px;"><p style="margin:0 0 4px 0;color:#64748b;font-size:12px;text-transform:uppercase;letter-spacing:0.5px;">Method</p><p style="margin:0;color:#1e293b;font-size:14px;">%s</p></td></tr>`, html.EscapeString(ev.Request.Method)))
		}
		b.WriteString(`</table></td></tr></table>`)
	}

	// Metadata grid
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#f8fafc;border-radius:6px;margin-bottom:28px;"><tr><td style="padding:20px;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0">`)
	b.WriteString(metaRow("ID", fp, true, "Time", now, false, false))
	b.WriteString(metaRow("Environment", ev.Environment, false, "Release", ev.Release, true, true))
	b.WriteString(metaRow("Level", level, false, "Platform", ev.Platform, false, true))
	if ev.ServerName != "" {
		b.WriteString(fmt.Sprintf(`<tr><td colspan="2" style="vertical-align:top;padding-top:16px;"><p style="margin:0 0 4px 0;color:#64748b;font-size:12px;text-transform:uppercase;letter-spacing:0.5px;">Server</p><p style="margin:0;color:#1e293b;font-size:14px;">%s</p></td></tr>`, html.EscapeString(ev.ServerName)))
	}
	b.WriteString(`</table></td></tr></table>`)

	// User
	userHTML := renderUser(ev.User)
	if userHTML != "" {
		b.WriteString(sectionLabel("User"))
		b.WriteString(userHTML)
	}

	// Breadcrumbs
	bcHTML := renderBreadcrumbs(ev.Breadcrumbs)
	if bcHTML != "" {
		b.WriteString(sectionLabel("Breadcrumbs"))
		b.WriteString(bcHTML)
	}

	// Tags
	if len(ev.Tags) > 0 {
		b.WriteString(sectionLabel("Tags"))
		b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="margin-bottom:28px;"><tr><td>`)
		keys := make([]string, 0, len(ev.Tags))
		for k := range ev.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf(`<span style="display:inline-block;padding:4px 10px;margin:0 6px 6px 0;background-color:#f1f5f9;border-radius:4px;font-size:13px;"><span style="color:#64748b;">%s</span> <span style="color:#1e293b;font-weight:500;">= %s</span></span>`,
				html.EscapeString(k), html.EscapeString(ev.Tags[k])))
		}
		b.WriteString(`</td></tr></table>`)
	}

	// Content end
	b.WriteString(`</td></tr>`)

	// CLI hint
	b.WriteString(`<tr><td style="padding:0 40px 32px 40px;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#f0f9ff;border:1px solid #bae6fd;border-radius:6px;">`)
	b.WriteString(fmt.Sprintf(`<tr><td style="padding:16px 20px;"><p style="margin:0 0 8px 0;color:#0369a1;font-size:13px;font-weight:600;">Investigate</p><p style="margin:0 0 4px 0;color:#1e293b;font-size:13px;font-family:'SF Mono',Monaco,'Courier New',monospace;">drillip show %s</p><p style="margin:0;color:#1e293b;font-size:13px;font-family:'SF Mono',Monaco,'Courier New',monospace;">drillip correlate %s</p></td></tr>`, fpShort, fpShort))
	b.WriteString(`</table></td></tr>`)

	// Footer
	footer := "drillip error tracking"
	if ev.ServerName != "" {
		footer += " &middot; " + html.EscapeString(ev.ServerName)
	}
	b.WriteString(fmt.Sprintf(`<tr><td style="padding:20px 40px;background-color:#f8fafc;border-top:1px solid #e2e8f0;border-radius:0 0 8px 8px;"><table role="presentation" width="100%%" cellspacing="0" cellpadding="0"><tr><td style="color:#94a3b8;font-size:12px;">%s</td></tr></table></td></tr>`, footer))

	// Close tables
	b.WriteString(`</table></td></tr></table></body></html>`)

	return b.String()
}

func sectionLabel(label string) string {
	return fmt.Sprintf(`<table role="presentation" width="100%%" cellspacing="0" cellpadding="0"><tr><td style="color:#64748b;font-size:12px;text-transform:uppercase;letter-spacing:0.5px;padding-bottom:10px;">%s</td></tr></table>`, label)
}

func metaRow(label1, value1 string, mono1 bool, label2, value2 string, mono2 bool, padTop bool) string {
	pt := ""
	if padTop {
		pt = "padding-top:16px;"
	}
	font1, font2 := "", ""
	if mono1 {
		font1 = "font-family:'SF Mono',Monaco,'Courier New',monospace;"
	}
	if mono2 {
		font2 = "font-family:'SF Mono',Monaco,'Courier New',monospace;"
	}
	return fmt.Sprintf(`<tr><td width="50%%" style="vertical-align:top;padding-right:12px;%s"><p style="margin:0 0 4px 0;color:#64748b;font-size:12px;text-transform:uppercase;letter-spacing:0.5px;">%s</p><p style="margin:0;color:#1e293b;font-size:14px;%s">%s</p></td><td width="50%%" style="vertical-align:top;padding-left:12px;%s"><p style="margin:0 0 4px 0;color:#64748b;font-size:12px;text-transform:uppercase;letter-spacing:0.5px;">%s</p><p style="margin:0;color:#1e293b;font-size:14px;%s">%s</p></td></tr>`,
		pt, label1, font1, html.EscapeString(value1),
		pt, label2, font2, html.EscapeString(value2))
}

func renderUser(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var u struct {
		IP       string `json:"ip_address"`
		Username string `json:"username"`
		Email    string `json:"email"`
		ID       string `json:"id"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return ""
	}
	if u.IP == "" && u.Username == "" && u.Email == "" && u.ID == "" {
		return ""
	}

	var pairs []struct{ label, value string }
	if u.IP != "" {
		pairs = append(pairs, struct{ label, value string }{"IP Address", u.IP})
	}
	if u.Username != "" {
		pairs = append(pairs, struct{ label, value string }{"Username", u.Username})
	}
	if u.Email != "" {
		pairs = append(pairs, struct{ label, value string }{"Email", u.Email})
	}
	if u.ID != "" {
		pairs = append(pairs, struct{ label, value string }{"User ID", u.ID})
	}

	var b strings.Builder
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="margin-bottom:28px;"><tr><td style="padding:16px 20px;background-color:#f8fafc;border-radius:6px;">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr>`)

	for i, p := range pairs {
		if i >= 2 {
			break // max 2 per row
		}
		b.WriteString(fmt.Sprintf(`<td width="50%%" style="vertical-align:top;"><p style="margin:0 0 4px 0;color:#64748b;font-size:12px;text-transform:uppercase;letter-spacing:0.5px;">%s</p><p style="margin:0;color:#1e293b;font-size:14px;font-family:'SF Mono',Monaco,'Courier New',monospace;">%s</p></td>`, p.label, html.EscapeString(p.value)))
	}
	b.WriteString(`</tr></table></td></tr></table>`)
	return b.String()
}

type breadcrumb struct {
	Timestamp string `json:"timestamp"`
	Category  string `json:"category"`
	Message   string `json:"message"`
	Level     string `json:"level"`
	Type      string `json:"type"`
}

var categoryColors = map[string][2]string{
	"http":    {"#dbeafe", "#1e40af"},
	"query":   {"#fef3c7", "#92400e"},
	"error":   {"#fee2e2", "#991b1b"},
	"default": {"#f1f5f9", "#475569"},
}

func renderBreadcrumbs(bd *domain.BreadcrumbData) string {
	if bd == nil || len(bd.Values) == 0 {
		return ""
	}

	var crumbs []breadcrumb
	for _, raw := range bd.Values {
		var bc breadcrumb
		if json.Unmarshal(raw, &bc) == nil && bc.Message != "" {
			crumbs = append(crumbs, bc)
		}
	}
	if len(crumbs) == 0 {
		return ""
	}

	// Show last 5
	if len(crumbs) > 5 {
		crumbs = crumbs[len(crumbs)-5:]
	}

	var b strings.Builder
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="margin-bottom:28px;"><tr><td>`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="border:1px solid #e2e8f0;border-radius:6px;overflow:hidden;">`)

	// Header
	b.WriteString(`<tr style="background-color:#f8fafc;">`)
	b.WriteString(`<td style="padding:8px 16px;color:#64748b;font-size:12px;font-weight:600;width:70px;">Time</td>`)
	b.WriteString(`<td style="padding:8px 16px;color:#64748b;font-size:12px;font-weight:600;width:60px;">Type</td>`)
	b.WriteString(`<td style="padding:8px 16px;color:#64748b;font-size:12px;font-weight:600;">Message</td>`)
	b.WriteString(`</tr>`)

	for _, bc := range crumbs {
		ts := formatBreadcrumbTime(bc.Timestamp)
		cat := bc.Category
		if cat == "" {
			cat = bc.Type
		}
		if cat == "" {
			cat = "info"
		}
		colors, ok := categoryColors[cat]
		if !ok {
			colors = categoryColors["default"]
		}

		b.WriteString(`<tr>`)
		b.WriteString(fmt.Sprintf(`<td style="padding:8px 16px;color:#94a3b8;font-size:13px;font-family:'SF Mono',Monaco,'Courier New',monospace;border-top:1px solid #f1f5f9;">%s</td>`, ts))
		b.WriteString(fmt.Sprintf(`<td style="padding:8px 16px;border-top:1px solid #f1f5f9;"><span style="display:inline-block;padding:2px 6px;background-color:%s;color:%s;font-size:11px;border-radius:3px;font-weight:500;">%s</span></td>`, colors[0], colors[1], html.EscapeString(cat)))
		// Truncate long messages
		msg := bc.Message
		if len(msg) > 80 {
			msg = msg[:77] + "..."
		}
		b.WriteString(fmt.Sprintf(`<td style="padding:8px 16px;color:#1e293b;font-size:13px;border-top:1px solid #f1f5f9;">%s</td>`, html.EscapeString(msg)))
		b.WriteString(`</tr>`)
	}

	b.WriteString(`</table></td></tr></table>`)
	return b.String()
}

func formatBreadcrumbTime(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
	}
	if err != nil {
		// Try unix timestamp as float
		return ts
	}
	return t.UTC().Format("15:04:05")
}

// --- Plain text fallback ---

// formatDuration returns a human-readable duration string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		if h == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", h)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func formatPlainEmail(ev *domain.Event, fp, project string, isRegression bool, resolvedFor time.Duration) string {
	evType, evValue := extractException(ev)
	level := ev.EffectiveLevel()
	now := time.Now().UTC().Format(time.RFC3339)
	fpShort := fp
	if len(fpShort) > 8 {
		fpShort = fpShort[:8]
	}

	var b strings.Builder

	if isRegression {
		if resolvedFor > 0 {
			b.WriteString(fmt.Sprintf("STATUS: REGRESSION (was resolved for %s)\n\n", formatDuration(resolvedFor)))
		} else {
			b.WriteString("STATUS: REGRESSION\n\n")
		}
	}

	if project != "" {
		b.WriteString(fmt.Sprintf("Project: %s\n", project))
	}
	b.WriteString(fmt.Sprintf("Type:        %s\n", evType))
	b.WriteString(fmt.Sprintf("Value:       %s\n", evValue))
	b.WriteString(fmt.Sprintf("Level:       %s\n", level))
	b.WriteString(fmt.Sprintf("Fingerprint: %s\n", fp))
	if ev.EventID != "" {
		b.WriteString(fmt.Sprintf("Event ID:    %s\n", ev.EventID))
	}

	if ev.Exception != nil && len(ev.Exception.Values) > 0 {
		exc := ev.Exception.Values[0]
		if exc.Stacktrace != nil && len(exc.Stacktrace.Frames) > 0 {
			b.WriteString("\nStacktrace:\n")
			for i := len(exc.Stacktrace.Frames) - 1; i >= 0; i-- {
				f := exc.Stacktrace.Frames[i]
				b.WriteString(fmt.Sprintf("  %s (%s:%d)\n", f.Function, f.Filename, f.Lineno))
			}
		}
	}

	if ev.Request != nil && ev.Request.URL != "" {
		b.WriteString(fmt.Sprintf("\nRequest: %s %s\n", ev.Request.Method, ev.Request.URL))
	}

	b.WriteString("\n")
	if ev.Environment != "" {
		b.WriteString(fmt.Sprintf("Environment: %s\n", ev.Environment))
	}
	if ev.Release != "" {
		b.WriteString(fmt.Sprintf("Release:     %s\n", ev.Release))
	}
	if ev.ServerName != "" {
		b.WriteString(fmt.Sprintf("Server:      %s\n", ev.ServerName))
	}
	if ev.Platform != "" {
		b.WriteString(fmt.Sprintf("Platform:    %s\n", ev.Platform))
	}
	b.WriteString(fmt.Sprintf("Time:        %s\n", now))

	if len(ev.Tags) > 0 {
		b.WriteString("\nTags:\n")
		keys := make([]string, 0, len(ev.Tags))
		for k := range ev.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("  %s = %s\n", k, ev.Tags[k]))
		}
	}

	b.WriteString(fmt.Sprintf("\n---\nInvestigate:\n  drillip show %s\n  drillip correlate %s\n", fpShort, fpShort))

	return b.String()
}

// --- Header sanitization ---

// sanitizeHeader strips CR and LF characters to prevent SMTP header injection
// and truncates to 200 characters.
func sanitizeHeader(s string) string {
	s = strings.NewReplacer("\r", "", "\n", "").Replace(s)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// --- MIME ---

func buildMultipartMIME(from, to, subject, textBody, htmlBody string) []byte {
	boundary := "drillip-boundary-" + fmt.Sprintf("%d", time.Now().UnixNano())

	var b strings.Builder
	b.WriteString(fmt.Sprintf("From: %s\r\n", sanitizeHeader(from)))
	b.WriteString(fmt.Sprintf("To: %s\r\n", sanitizeHeader(to)))
	b.WriteString(fmt.Sprintf("Subject: %s\r\n", sanitizeHeader(subject)))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
	b.WriteString("\r\n")

	// Plain text part
	b.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(textBody)
	b.WriteString("\r\n")

	// HTML part
	b.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	b.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	b.WriteString("\r\n")

	b.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	return []byte(b.String())
}
