package notify

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net"
	"net/smtp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PhilHem/drillip/domain"
	"github.com/PhilHem/drillip/store"
)

// SMTPConfig holds email notification settings.
// Notifications are disabled when Host or To is empty.
type SMTPConfig struct {
	Host string // SMTP server hostname
	Port string // SMTP server port (default "25")
	From string // sender address
	To   string // recipient address
	User string // optional SMTP username
	Pass string // optional SMTP password
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
	SMTP     SMTPConfig
	Project  string
	Cooldown time.Duration
	Store    *store.Store // for silence checks

	mu       sync.Mutex
	lastSent time.Time
	recent   map[string]time.Time // fingerprint -> last notified

	// now and sendMail are injectable for testing.
	now      func() time.Time
	sendMail func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// NewNotifier creates a Notifier with the given SMTP config, project name, cooldown duration, and store.
func NewNotifier(smtpCfg SMTPConfig, project string, cooldown time.Duration, st *store.Store) *Notifier {
	return &Notifier{
		SMTP:     smtpCfg,
		Project:  project,
		Cooldown: cooldown,
		Store:    st,
		recent:   make(map[string]time.Time),
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
// Safe to call from a goroutine.
func (n *Notifier) NotifyNewError(ev *domain.Event, fp string, regression bool, resolvedFor time.Duration) {
	if !n.SMTP.Enabled() {
		return
	}

	if n.Store != nil && n.Store.IsSilenced(fp) {
		log.Printf("notify: silenced fingerprint %s, skipping", fp[:8])
		return
	}

	n.mu.Lock()
	ok := n.shouldNotify(fp)
	n.mu.Unlock()

	if !ok {
		log.Printf("notify: throttled notification for fingerprint %s", fp)
		return
	}

	evType, _ := extractException(ev)
	if regression {
		subject := fmt.Sprintf("[drillip] regression: %s", evType)
		htmlBody := formatHTMLEmail(ev, fp, n.Project, true, resolvedFor)
		textBody := formatPlainEmail(ev, fp, n.Project, true, resolvedFor)
		msg := buildMultipartMIME(n.SMTP.From, n.SMTP.To, subject, textBody, htmlBody)

		var auth smtp.Auth
		if n.SMTP.User != "" {
			auth = smtp.PlainAuth("", n.SMTP.User, n.SMTP.Pass, n.SMTP.Host)
		}
		send := smtp.SendMail
		if n.sendMail != nil {
			send = n.sendMail
		}
		if err := send(n.SMTP.Addr(), auth, n.SMTP.From, []string{n.SMTP.To}, msg); err != nil {
			log.Printf("notify: send mail: %v", err)
		}
		return
	}

	subject := fmt.Sprintf("[drillip] %s: %s", ev.EffectiveLevel(), evType)
	htmlBody := formatHTMLEmail(ev, fp, n.Project, false, 0)
	textBody := formatPlainEmail(ev, fp, n.Project, false, 0)
	msg := buildMultipartMIME(n.SMTP.From, n.SMTP.To, subject, textBody, htmlBody)

	var auth smtp.Auth
	if n.SMTP.User != "" {
		auth = smtp.PlainAuth("", n.SMTP.User, n.SMTP.Pass, n.SMTP.Host)
	}

	send := smtp.SendMail
	if n.sendMail != nil {
		send = n.sendMail
	}

	if err := send(n.SMTP.Addr(), auth, n.SMTP.From, []string{n.SMTP.To}, msg); err != nil {
		log.Printf("notify: send mail: %v", err)
	}
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
	headerGradient := `linear-gradient(135deg,#e74c3c 0%%,#c0392b 100%%)`
	headerLabel := "New Issue"
	exceptionBg := "#fef2f2"
	exceptionBorder := "#e74c3c"
	if isRegression {
		headerGradient = `linear-gradient(135deg,#f59e0b 0%%,#d97706 100%%)`
		headerLabel = "Regression"
		exceptionBg = "#fffbeb"
		exceptionBorder = "#f59e0b"
	}

	var b strings.Builder

	// Document start + outer table
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>`)
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
	b.WriteString(fmt.Sprintf(`<tr><td style="background:%s;padding:24px 40px;">`, headerGradient))
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr>`)
	b.WriteString(fmt.Sprintf(`<td><p style="margin:0;color:rgba(255,255,255,0.8);font-size:13px;text-transform:uppercase;letter-spacing:0.5px;">%s</p><p style="margin:8px 0 0 0;color:#ffffff;font-size:20px;font-weight:600;">%s</p></td>`, headerLabel, html.EscapeString(evType)))
	b.WriteString(fmt.Sprintf(`<td align="right" style="vertical-align:top;"><span style="display:inline-block;padding:4px 12px;border-radius:12px;background-color:rgba(255,255,255,0.2);color:#fff;font-size:12px;font-weight:500;">%s</span></td>`, html.EscapeString(level)))
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

// --- MIME ---

func buildMultipartMIME(from, to, subject, textBody, htmlBody string) []byte {
	boundary := "drillip-boundary-" + fmt.Sprintf("%d", time.Now().UnixNano())

	var b strings.Builder
	b.WriteString(fmt.Sprintf("From: %s\r\n", from))
	b.WriteString(fmt.Sprintf("To: %s\r\n", to))
	b.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
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
