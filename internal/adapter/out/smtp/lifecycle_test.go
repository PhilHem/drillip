package smtp

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

func TestCloseJoinsAcceptedNotifications(t *testing.T) {
	for _, mode := range []string{"immediate", "buffered digest", "running digest", "resolved", "test email"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var callbacks atomic.Int32
				var calls atomic.Int32
				n := NewNotifier(SMTPConfig{Host: "localhost", From: "sender@example.com", To: "ops@example.com"}, "project", 0, 0, func(string) { callbacks.Add(1) })
				release := make(chan struct{})
				n.sendMail = func(ctx context.Context, _ string, _ smtp.Auth, _ string, _ []string, _ []byte) error {
					calls.Add(1)
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				testDone := make(chan error, 1)
				switch mode {
				case "resolved":
					n.NotifyResolved([]domain.ResolvedError{{Fingerprint: "resolved"}})
				case "test email":
					go func() { testDone <- n.SendTestEmail() }()
				default:
					if mode != "immediate" {
						n.Digest = time.Hour
					}
					n.NotifyNewError(&domain.Event{Message: "first"}, "first", false, 0)
					if mode != "immediate" {
						n.NotifyNewError(&domain.Event{Message: "second"}, "second", false, 0)
					}
					if mode == "running digest" {
						n.flush()
					}
				}
				synctest.Wait()
				closed := make(chan struct{})
				go func() { n.Close(); close(closed) }()
				synctest.Wait()
				select {
				case <-closed:
					t.Fatal("Close returned while delivery was active")
				default:
				}
				close(release)
				<-closed
				if mode == "test email" {
					if err := <-testDone; err != nil {
						t.Fatal(err)
					}
				}
				wantCallbacks := int32(1)
				if strings.Contains(mode, "digest") {
					wantCallbacks = 2
				}
				if mode == "resolved" || mode == "test email" {
					wantCallbacks = 0
				}
				if callbacks.Load() != wantCallbacks {
					t.Fatalf("callbacks = %d, want %d", callbacks.Load(), wantCallbacks)
				}
				if calls.Load() != 1 {
					t.Fatalf("deliveries = %d, want 1", calls.Load())
				}
				// Close is terminal: no timer, late producer, or repeated Close can add work.
				n.NotifyNewError(&domain.Event{Message: "late"}, "late", false, 0)
				n.NotifyResolved([]domain.ResolvedError{{Fingerprint: "late"}})
				if err := n.SendTestEmail(); err == nil {
					t.Fatal("test email accepted after Close")
				}
				n.Close()
				time.Sleep(2 * time.Hour)
				synctest.Wait()
				if calls.Load() != 1 || callbacks.Load() != wantCallbacks {
					t.Fatal("work ran after Close returned")
				}
			})
		})
	}
}

func TestCloseCancelsStalledDeliveryAndRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var marked atomic.Bool
		n := NewNotifier(SMTPConfig{Host: "localhost", To: "ops@example.com"}, "project", 0, 0, func(string) { marked.Store(true) })
		var attempts atomic.Int32
		n.sendMail = func(ctx context.Context, _ string, _ smtp.Auth, _ string, _ []string, _ []byte) error {
			attempts.Add(1)
			<-ctx.Done()
			return ctx.Err()
		}
		n.NotifyNewError(&domain.Event{Message: "blocked"}, "blocked", false, 0)
		start := time.Now()
		n.Close()
		if elapsed := time.Since(start); elapsed != notificationDrainTimeout {
			t.Fatalf("drain took %s", elapsed)
		}
		if attempts.Load() != 2 {
			t.Fatalf("attempts = %d, want interrupted second attempt", attempts.Load())
		}
		if marked.Load() {
			t.Fatal("failed delivery marked as notified")
		}
		time.Sleep(time.Minute)
		if attempts.Load() != 2 {
			t.Fatal("retry survived Close")
		}
	})
}

func TestConcurrentNotifyAndClose(t *testing.T) {
	var closed atomic.Bool
	var callbacks atomic.Int32
	n := NewNotifier(SMTPConfig{Host: "localhost", To: "ops@example.com"}, "project", 0, time.Millisecond, func(string) {
		if closed.Load() {
			t.Error("callback after Close")
		}
		callbacks.Add(1)
	})
	n.sendMail = func(context.Context, string, smtp.Auth, string, []string, []byte) error { return nil }
	var producers sync.WaitGroup
	for i := 0; i < 100; i++ {
		producers.Add(1)
		go func(i int) {
			defer producers.Done()
			n.NotifyNewError(&domain.Event{Message: "concurrent"}, fmt.Sprint(i), false, 0)
			n.NotifyResolved([]domain.ResolvedError{{Fingerprint: "resolved"}})
		}(i)
	}
	n.Close()
	closed.Store(true)
	producers.Wait()
	count := callbacks.Load()
	n.Close()
	if callbacks.Load() != count {
		t.Fatal("callbacks survived Close")
	}
}

func TestSMTPTransportBoundsStalledGreetingAndProtocol(t *testing.T) {
	for _, stage := range []string{"greeting", "data"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				reader := bufio.NewReader(conn)
				if stage == "data" {
					fmt.Fprint(conn, "220 localhost ready\r\n")
					for {
						line, err := reader.ReadString('\n')
						if err != nil {
							return
						}
						if strings.HasPrefix(line, "DATA") {
							break
						}
						fmt.Fprint(conn, "250 localhost OK\r\n")
					}
				}
				// Never send the greeting / DATA response; wait until the client closes.
				_, _ = reader.ReadByte()
			}()
			host, port, _ := net.SplitHostPort(listener.Addr().String())
			n := NewNotifier(SMTPConfig{Host: host, Port: port, From: "sender@example.com", To: "ops@example.com"}, "project", 0, 0, nil)
			defer n.Close()
			start := time.Now()
			if err := n.SendTestEmail(); err == nil {
				t.Fatal("stalled SMTP succeeded")
			}
			if elapsed := time.Since(start); elapsed < 4*time.Second || elapsed > 7*time.Second {
				t.Fatalf("SMTP attempt took %s, want approximately five seconds", elapsed)
			}
			<-serverDone
		})
	}
}
