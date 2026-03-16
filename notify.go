package main

import (
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"
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

func (c SMTPConfig) enabled() bool {
	return c.Host != "" && c.To != ""
}

func (c SMTPConfig) addr() string {
	port := c.Port
	if port == "" {
		port = "25"
	}
	return net.JoinHostPort(c.Host, port)
}

// notifyNewError sends an email for a newly seen error.
// Must be called in a goroutine — never blocks ingest.
func notifyNewError(cfg SMTPConfig, ev *Event, fp string) {
	if !cfg.enabled() {
		return
	}

	subject, body := formatEmail(ev, fp)
	msg := buildMIME(cfg.From, cfg.To, subject, body)

	var auth smtp.Auth
	if cfg.User != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	}

	if err := smtp.SendMail(cfg.addr(), auth, cfg.From, []string{cfg.To}, msg); err != nil {
		log.Printf("notify: send mail: %v", err)
	}
}

func formatEmail(ev *Event, fp string) (subject, body string) {
	level := ev.EffectiveLevel()

	var evType, evValue, location string
	if ev.Exception != nil && len(ev.Exception.Values) > 0 {
		exc := ev.Exception.Values[0]
		evType = exc.Type
		evValue = exc.Value
		if exc.Stacktrace != nil && len(exc.Stacktrace.Frames) > 0 {
			f := exc.Stacktrace.Frames[len(exc.Stacktrace.Frames)-1]
			location = fmt.Sprintf("%s in %s, line %d", f.Filename, f.Function, f.Lineno)
		}
	} else {
		evType = "message"
		evValue = ev.messageText()
	}

	subject = fmt.Sprintf("[drillip] %s: %s", level, evType)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Type:        %s\n", evType))
	b.WriteString(fmt.Sprintf("Value:       %s\n", evValue))
	b.WriteString(fmt.Sprintf("Level:       %s\n", level))
	b.WriteString(fmt.Sprintf("Fingerprint: %s\n", fp))
	if location != "" {
		b.WriteString(fmt.Sprintf("Location:    %s\n", location))
	}
	if ev.Environment != "" {
		b.WriteString(fmt.Sprintf("Environment: %s\n", ev.Environment))
	}
	if ev.Release != "" {
		b.WriteString(fmt.Sprintf("Release:     %s\n", ev.Release))
	}
	b.WriteString(fmt.Sprintf("Time:        %s\n", time.Now().UTC().Format(time.RFC3339)))

	return subject, b.String()
}

func buildMIME(from, to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("From: %s\r\n", from))
	b.WriteString(fmt.Sprintf("To: %s\r\n", to))
	b.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}
