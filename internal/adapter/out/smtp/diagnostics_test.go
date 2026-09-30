package smtp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

func TestSMTPDiagnosesRelayFailures(t *testing.T) {
	for _, tc := range []struct {
		name, stage, reply, code string
		stallCleanup             bool
	}{
		{"authentication rejected", "AUTH", "535 5.7.8 provider-private authentication failure", "smtp_auth_rejected", false},
		{"authentication temporarily unavailable", "AUTH", "454 4.7.0 service unavailable", "smtp_auth_failed", false},
		{"authentication unsupported", "AUTH", "504 mechanism unsupported", "smtp_auth_failed", false},
		{"authentication disconnected", "AUTH", "disconnect", "smtp_connection_failed", false},
		{"authentication timed out", "AUTH", "timeout", "smtp_timeout", false},
		{"authentication rejection survives cleanup timeout", "AUTH", "535 credentials rejected", "smtp_auth_rejected", true},
		{"sender rejected", "MAIL", "550 sender not permitted", "smtp_sender_rejected", false},
		{"recipient rejected", "RCPT", "550 mailbox unavailable", "smtp_recipient_rejected", false},
		{"recipient temporarily unavailable", "RCPT", "450 mailbox unavailable", "smtp_recipient_rejected", false},
		{"sender protocol failure", "MAIL", "503 wrong sequence", "smtp_delivery_failed", false},
		{"recipient service failure", "RCPT", "421 service unavailable", "smtp_delivery_failed", false},
		{"greeting rejection is not authentication rejection", "HELLO", "550 greeting refused", "smtp_delivery_failed", false},
		{"message rejected", "DATA", "554 message refused", "smtp_delivery_failed", false},
		{"quit failure after acceptance", "QUIT", "500 cannot quit", "smtp_delivery_failed", false},
		{"quit disconnected after acceptance", "QUIT", "disconnect", "smtp_connection_failed", false},
		{"completion disconnected after acceptance", "COMPLETION", "disconnect", "smtp_connection_failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * smtpAttemptTimeout))
				reader := bufio.NewReader(conn)
				fmt.Fprint(conn, "220 localhost ready\r\n")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						if errors.Is(err, io.EOF) {
							err = nil
						}
						done <- err
						return
					}
					op := strings.Fields(line)[0]
					if op == "EHLO" || op == "HELO" {
						op = "HELLO"
					}
					if op == tc.stage {
						switch tc.reply {
						case "disconnect":
							done <- nil
							return
						case "timeout":
							_, _ = io.Copy(io.Discard, reader)
							done <- nil
							return
						default:
							fmt.Fprint(conn, tc.reply+"\r\n")
							continue
						}
					}
					switch op {
					case "HELLO":
						fmt.Fprint(conn, "250-localhost\r\n250 AUTH PLAIN\r\n")
					case "AUTH":
						fmt.Fprint(conn, "235 authenticated\r\n")
					case "*":
						if tc.stallCleanup {
							_, _ = io.Copy(io.Discard, reader)
							done <- nil
							return
						}
						fmt.Fprint(conn, "501 authentication canceled\r\n")
					case "MAIL", "RCPT":
						fmt.Fprint(conn, "250 accepted\r\n")
					case "DATA":
						fmt.Fprint(conn, "354 send message\r\n")
						if _, err := textproto.NewReader(reader).ReadDotBytes(); err != nil {
							done <- err
							return
						}
						if tc.stage == "COMPLETION" {
							done <- nil
							return
						}
						fmt.Fprint(conn, "250 queued\r\n")
					case "QUIT":
						fmt.Fprint(conn, "221 goodbye\r\n")
						done <- nil
						return
					default:
						done <- fmt.Errorf("unexpected command %q", line)
						return
					}
				}
			}()
			host, port, _ := net.SplitHostPort(listener.Addr().String())
			n := NewNotifier(SMTPConfig{Host: host, Port: port, User: "test-user", Pass: "private-password", From: "sender@example.com", To: "ops@example.com"}, "diagnosis", 0, 0, nil)
			defer n.Close()
			err = n.SendTestEmail()
			if relayErr := <-done; relayErr != nil {
				t.Fatal(relayErr)
			}
			var diagnosis *domain.NotificationError
			if !errors.As(err, &diagnosis) || diagnosis.Code != tc.code {
				t.Fatalf("diagnosis = %v; want code %s", err, tc.code)
			}
			if diagnosis.Message == "" || diagnosis.Hint == "" || strings.Contains(diagnosis.Message+diagnosis.Hint, "private") {
				t.Fatalf("missing or unsafe operator diagnosis: %+v", diagnosis)
			}
			if strings.HasPrefix(tc.reply, "535") {
				var response *textproto.Error
				if !errors.As(err, &response) || response.Code != 535 {
					t.Fatalf("lost original SMTP rejection: %v", err)
				}
			}
			if (tc.stage == "QUIT" || tc.stage == "COMPLETION") && !strings.Contains(diagnosis.Hint, "may have been accepted") {
				t.Fatalf("unsafe advice after acceptance: %s", diagnosis.Hint)
			}
		})
	}
}

func TestSMTPInjectedFailurePreservesCauseWithoutGuessingFromText(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", From: "sender@example.com", To: "ops@example.com"}, "diagnosis", 0, 0, nil)
	defer n.Close()
	cause := errors.New("535 password private-password rejected; TLS certificate invalid")
	n.SetSendMail(func(context.Context, string, smtp.Auth, string, []string, []byte) error {
		return fmt.Errorf("delivery wrapper: %w", cause)
	})
	err := n.SendTestEmail()
	var diagnosis *domain.NotificationError
	if !errors.As(err, &diagnosis) || diagnosis.Code != "smtp_delivery_failed" || !errors.Is(err, cause) {
		t.Fatalf("lost cause or guessed diagnosis from text: %v", err)
	}
	if strings.Contains(diagnosis.Message+diagnosis.Hint, "private-password") || !strings.Contains(err.Error(), "private-password") {
		t.Fatalf("operator diagnosis must omit raw cause while logs retain it: %+v", diagnosis)
	}
}

func TestSMTPClosedNotifierHasLifecycleDiagnosis(t *testing.T) {
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "ops@example.com"}, "diagnosis", 0, 0, nil)
	n.Close()
	var diagnosis *domain.NotificationError
	if err := n.SendTestEmail(); !errors.As(err, &diagnosis) || diagnosis.Code != "notifications_unavailable" {
		t.Fatalf("closed notifier: %v", err)
	}
}
