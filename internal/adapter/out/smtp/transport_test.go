package smtp

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

func TestSMTPTransportDeliversAndVerifiesTLS(t *testing.T) {
	for _, mode := range []string{"plain relay", "untrusted TLS rejected", "explicit TLS exception with auth"} {
		t.Run(mode, func(t *testing.T) {
			certificateServer := httptest.NewTLSServer(nil)
			certificates := certificateServer.TLS.Certificates
			certificateServer.Close()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			type transaction struct {
				commands []string
				body     string
				auth     string
				err      error
			}
			result := make(chan transaction, 1)
			go func() {
				var tx transaction
				defer func() { result <- tx }()
				conn, err := listener.Accept()
				if err != nil {
					tx.err = err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(conn)
				fmt.Fprint(conn, "220 localhost ready\r\n")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						tx.err = err
						return
					}
					line = strings.TrimSpace(line)
					tx.commands = append(tx.commands, line)
					switch {
					case strings.HasPrefix(line, "EHLO"):
						if mode == "plain relay" {
							fmt.Fprint(conn, "250 localhost\r\n")
						} else {
							fmt.Fprint(conn, "250-localhost\r\n250-STARTTLS\r\n250 AUTH PLAIN\r\n")
						}
					case line == "STARTTLS":
						fmt.Fprint(conn, "220 begin TLS\r\n")
						secure := tls.Server(conn, &tls.Config{Certificates: certificates})
						if err := secure.Handshake(); err != nil {
							tx.err = err
							return
						}
						conn = secure
						reader = bufio.NewReader(conn)
					case strings.HasPrefix(line, "AUTH PLAIN "):
						decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
						if err != nil {
							tx.err = err
							return
						}
						tx.auth = string(decoded)
						fmt.Fprint(conn, "235 authenticated\r\n")
					case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
						fmt.Fprint(conn, "250 accepted\r\n")
					case line == "DATA":
						fmt.Fprint(conn, "354 send message\r\n")
						body, err := textproto.NewReader(reader).ReadDotBytes()
						if err != nil {
							tx.err = err
							return
						}
						tx.body = string(body)
						fmt.Fprint(conn, "250 queued\r\n")
					case line == "QUIT":
						fmt.Fprint(conn, "221 goodbye\r\n")
						return
					default:
						tx.err = fmt.Errorf("unexpected command %q", line)
						return
					}
				}
			}()
			host, port, _ := net.SplitHostPort(listener.Addr().String())
			cfg := SMTPConfig{Host: host, Port: port, From: "sender@example.com", To: "ops@example.com"}
			if mode == "explicit TLS exception with auth" {
				cfg.SkipVerify = true
				cfg.User = "username"
				cfg.Pass = "password"
			}
			n := NewNotifier(cfg, "transport-test", 0, 0, nil)
			defer n.Close()
			err = n.SendTestEmail()
			tx := <-result
			if mode == "untrusted TLS rejected" {
				if err == nil || !strings.Contains(err.Error(), "certificate") {
					t.Fatalf("want certificate rejection, got %v", err)
				}
				if tx.body != "" {
					t.Fatal("sent message before certificate validation")
				}
				var diagnosis *domain.NotificationError
				var verification *tls.CertificateVerificationError
				if !errors.As(err, &diagnosis) || diagnosis.Code != "smtp_tls_failed" || !errors.As(err, &verification) {
					t.Fatalf("want TLS diagnosis preserving verification error, got %v", err)
				}
				if !strings.Contains(diagnosis.Hint, "keep certificate verification enabled") {
					t.Fatalf("TLS advice must preserve verification: %s", diagnosis.Hint)
				}
				return
			}
			if err != nil || tx.err != nil {
				t.Fatalf("client=%v relay=%v", err, tx.err)
			}
			commands := strings.Join(tx.commands, "\n")
			for _, want := range []string{"MAIL FROM:<sender@example.com>", "RCPT TO:<ops@example.com>", "DATA", "QUIT"} {
				if !strings.Contains(commands, want) {
					t.Errorf("missing %s in %q", want, commands)
				}
			}
			if !strings.Contains(tx.body, "Subject: [drillip] test email from transport-test") || !strings.Contains(tx.body, "This is a test email from drillip.") {
				t.Fatalf("unexpected message: %s", tx.body)
			}
			if mode == "explicit TLS exception with auth" {
				if tx.auth != "\x00username\x00password" {
					t.Fatalf("unexpected AUTH payload %q", tx.auth)
				}
				if !strings.Contains(commands, "STARTTLS") {
					t.Fatal("authentication skipped TLS")
				}
			}
		})
	}
}
