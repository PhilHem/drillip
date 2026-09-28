package notify

import (
	"errors"
	"fmt"
	"net/smtp"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

func TestNotificationMarkedOnlyAfterDelivery(t *testing.T) {
	for _, mode := range []struct {
		name   string
		digest time.Duration
		count  int
	}{
		{"immediate", 0, 1},
		{"single-item digest", time.Minute, 1},
		{"multi-item digest", time.Minute, 2},
	} {
		for _, failures := range []int{0, 1, 3} {
			name := fmt.Sprintf("%s/failures=%d", mode.name, failures)
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var marked []string
					delivered := false
					n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", 0, mode.digest, func(fp string) {
						if !delivered {
							t.Error("notification marked before successful SMTP delivery")
						}
						marked = append(marked, fp)
					})
					t.Cleanup(n.Close)
					attempts := 0
					n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
						attempts++
						if attempts <= failures {
							return errors.New("SMTP unavailable")
						}
						delivered = true
						return nil
					}

					fps := []string{"abcdef1234567890", "1234567890abcdef"}[:mode.count]
					for _, fp := range fps {
						n.NotifyNewError(&domain.Event{Message: "test"}, fp, false, 0)
					}
					if mode.digest > 0 && len(marked) != 0 {
						t.Fatal("buffered notifications marked before flush")
					}
					n.Close()
					if want := min(failures+1, 3); attempts != want {
						t.Errorf("SMTP attempts = %d, want %d", attempts, want)
					}
					var want []string
					if failures < 3 {
						want = fps
					}
					if !reflect.DeepEqual(marked, want) {
						t.Errorf("marked fingerprints = %v, want %v", marked, want)
					}
				})
			})
		}
	}
}

func TestDigestPreservesDistinctErrorsDuringCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var marked []string
		n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", time.Minute, 5*time.Minute, func(fp string) {
			mu.Lock()
			defer mu.Unlock()
			marked = append(marked, fp)
		})
		t.Cleanup(n.Close)
		var messages []string
		n.sendMail = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
			mu.Lock()
			defer mu.Unlock()
			messages = append(messages, string(msg))
			return nil
		}
		fps := []string{"abcdef1234567890", "1234567890abcdef", "abcdefabcdefabcd"}
		for i, fp := range fps {
			n.NotifyNewError(&domain.Event{Message: "test"}, fp, i == 2, time.Hour)
		}
		// Repeated occurrences of the same fingerprint remain throttled.
		n.NotifyNewError(&domain.Event{Message: "test"}, fps[0], false, 0)
		mu.Lock()
		sentEarly := len(messages) != 0 || len(marked) != 0
		mu.Unlock()
		if sentEarly {
			t.Fatal("digest sent before its window elapsed")
		}
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		mu.Lock()
		defer mu.Unlock()
		if len(messages) != 1 {
			t.Fatalf("sent %d messages, want one digest", len(messages))
		}
		if !strings.Contains(messages[0], "Subject: [drillip] 3 new errors in proj") {
			t.Error("digest does not contain all three distinct errors")
		}
		if !reflect.DeepEqual(marked, fps) {
			t.Errorf("marked fingerprints = %v, want %v", marked, fps)
		}
	})
}

func TestDigestWindowRespectsLongerCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := NewNotifier(SMTPConfig{Host: "localhost", To: "a@b.com", From: "x@y.com"}, "proj", time.Minute, 10*time.Second, nil)
		t.Cleanup(n.Close)
		var calls atomic.Int32
		n.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
			calls.Add(1)
			return nil
		}
		for _, fp := range []string{"abcdef1234567890", "1234567890abcdef"} {
			n.NotifyNewError(&domain.Event{Message: "test"}, fp, false, 0)
			time.Sleep(10 * time.Second)
			synctest.Wait()
			if calls.Load() != 0 {
				t.Fatal("digest sent before cooldown elapsed")
			}
			time.Sleep(50 * time.Second)
			synctest.Wait()
			if got := calls.Load(); got != 1 {
				t.Fatalf("sent %d messages, want one per cooldown window", got)
			}
			calls.Store(0)
		}
	})
}
