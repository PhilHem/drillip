package service

import (
	"errors"
	"reflect"
	"testing"
	"time"

	inport "github.com/PhilHem/drillip/internal/application/port/in"
	outport "github.com/PhilHem/drillip/internal/application/port/out"
	"github.com/PhilHem/drillip/internal/domain"
)

// Unused repository methods deliberately panic through the embedded nil port.
// These tests exercise orchestration without a database or network adapter.
type eventRepository struct {
	outport.Repository
	result   domain.StoreResult
	err      error
	silenced bool
	stored   int
	resolved domain.ResolveResult
}

func (r *eventRepository) StoreEvent(*domain.Event) (domain.StoreResult, error) {
	r.stored++
	return r.result, r.err
}

func (r *eventRepository) IsSilenced(string) bool { return r.silenced }

func (r *eventRepository) Resolve(string) (domain.ResolveResult, error) {
	return r.resolved, r.err
}

type notification struct {
	fingerprint string
	regression  bool
	resolvedFor time.Duration
}

type notificationRecorder struct {
	events   chan notification
	resolved chan []domain.ResolvedError
	err      error
}

func (n *notificationRecorder) NotifyNewError(_ *domain.Event, fp string, regression bool, duration time.Duration) {
	n.events <- notification{fp, regression, duration}
}

func (n *notificationRecorder) NotifyResolved(resolved []domain.ResolvedError) {
	n.resolved <- resolved
}

func (n *notificationRecorder) SendTestEmail() error { return n.err }
func (n *notificationRecorder) Recipient() string    { return "ops@example.com" }

func TestIngestUsesPortsAndPreservesNotificationPolicy(t *testing.T) {
	storeErr := errors.New("storage unavailable")
	for _, tc := range []struct {
		name       string
		event      domain.Event
		result     domain.StoreResult
		silenced   bool
		err        error
		wantNotify bool
	}{
		{name: "ignored empty event"},
		{name: "new", event: domain.Event{Message: "failure"}, result: domain.StoreResult{IsNew: true}, wantNotify: true},
		{name: "regression", event: domain.Event{Message: "failure"}, result: domain.StoreResult{IsRegression: true, ResolvedDuration: time.Hour}, wantNotify: true},
		{name: "duplicate", event: domain.Event{Message: "failure"}},
		{name: "silenced", event: domain.Event{Message: "failure"}, result: domain.StoreResult{IsNew: true}, silenced: true},
		{name: "storage error", event: domain.Event{Message: "failure"}, result: domain.StoreResult{IsNew: true}, err: storeErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.result.Fingerprint = "1234567890abcdef"
			repo := &eventRepository{result: tc.result, err: tc.err, silenced: tc.silenced}
			notifier := &notificationRecorder{events: make(chan notification, 1)}
			app := New(repo, notifier, nil)
			fp, err := app.Ingest(&tc.event)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if tc.event.Message == "" {
				if fp != "ok" || repo.stored != 0 {
					t.Fatalf("empty event: response = %q, writes = %d", fp, repo.stored)
				}
			} else if tc.err == nil && fp != tc.result.Fingerprint {
				t.Fatalf("fingerprint = %q", fp)
			}
			if tc.wantNotify {
				select {
				case got := <-notifier.events:
					want := notification{tc.result.Fingerprint, tc.result.IsRegression, tc.result.ResolvedDuration}
					if got != want {
						t.Fatalf("notification = %+v, want %+v", got, want)
					}
				case <-time.After(time.Second):
					t.Fatal("notification was not delivered")
				}
			} else {
				select {
				case got := <-notifier.events:
					t.Fatalf("unexpected notification: %+v", got)
				case <-time.After(20 * time.Millisecond):
				}
			}
		})
	}
}

func TestResolveNotifiesOnlyAfterSuccessfulChange(t *testing.T) {
	resolved := []domain.ResolvedError{{Fingerprint: "1234567890abcdef"}}
	for _, storeErr := range []error{nil, errors.New("transaction failed")} {
		repo := &eventRepository{resolved: domain.ResolveResult{Matched: 1, Resolved: resolved}, err: storeErr}
		notifier := &notificationRecorder{resolved: make(chan []domain.ResolvedError, 1)}
		_, err := New(repo, notifier, nil).Resolve("1234")
		if !errors.Is(err, storeErr) {
			t.Fatalf("error = %v, want %v", err, storeErr)
		}
		if storeErr != nil {
			select {
			case <-notifier.resolved:
				t.Fatal("notified after failed state change")
			case <-time.After(20 * time.Millisecond):
			}
			continue
		}
		select {
		case got := <-notifier.resolved:
			if !reflect.DeepEqual(got, resolved) {
				t.Fatalf("resolved = %+v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("resolved notification was not delivered")
		}
	}
}

func TestTestEmailReportsDisabledAndDeliveryErrors(t *testing.T) {
	if _, err := New(nil, nil, nil).SendTestEmail(); !errors.Is(err, inport.ErrNotificationsDisabled) {
		t.Fatalf("disabled error = %v", err)
	}
	notifier := &notificationRecorder{}
	app := New(nil, notifier, nil)
	if recipient, err := app.SendTestEmail(); err != nil || recipient != "ops@example.com" {
		t.Fatalf("recipient = %q, error = %v", recipient, err)
	}
	notifier.err = errors.New("delivery failed")
	if _, err := app.SendTestEmail(); !errors.Is(err, notifier.err) {
		t.Fatalf("delivery error = %v", err)
	}
}
