// Package service coordinates domain models and application ports.
package service

import (
	"log/slog"
	"time"

	inport "github.com/PhilHem/drillip/internal/application/port/in"
	outport "github.com/PhilHem/drillip/internal/application/port/out"
	"github.com/PhilHem/drillip/internal/domain"
)

// Errors implements the error ingestion, investigation, and management use cases.
type Errors struct {
	store     outport.Repository
	notifier  outport.Notifier
	telemetry outport.Telemetry
}

var (
	_ inport.Errors        = (*Errors)(nil)
	_ inport.Ingestor      = (*Errors)(nil)
	_ inport.Health        = (*Errors)(nil)
	_ inport.Notifications = (*Errors)(nil)
	_ inport.Correlator    = (*Errors)(nil)
)

// New connects persistence with optional notification and telemetry ports.
// Pass nil for disabled notifications or telemetry.
func New(store outport.Repository, notifier outport.Notifier, telemetry outport.Telemetry) *Errors {
	return &Errors{store: store, notifier: notifier, telemetry: telemetry}
}

func (s *Errors) Ingest(event *domain.Event) (string, error) {
	hasException := event.Exception != nil && len(event.Exception.Values) > 0
	if !hasException && event.MessageText() == "" {
		return "ok", nil
	}
	result, err := s.store.StoreEvent(event)
	if err != nil {
		return "", err
	}
	evType := "message"
	if hasException {
		evType = event.Exception.Values[0].Type
	}
	slog.Debug("event stored", "fingerprint", result.Fingerprint, "type", evType, "new", result.IsNew, "regression", result.IsRegression)
	if (result.IsNew || result.IsRegression) && s.notifier != nil {
		if s.store.IsSilenced(result.Fingerprint) {
			slog.Info("notify: silenced fingerprint, skipping", "fingerprint", result.Fingerprint[:8])
		} else {
			go s.notifier.NotifyNewError(event, result.Fingerprint, result.IsRegression, result.ResolvedDuration)
		}
	}
	return result.Fingerprint, nil
}

func (s *Errors) Resolve(prefix string) (domain.ResolveResult, error) {
	result, err := s.store.Resolve(prefix)
	if err == nil && s.notifier != nil && len(result.Resolved) > 0 {
		go s.notifier.NotifyResolved(result.Resolved)
	}
	return result, err
}

func (s *Errors) SendTestEmail() (string, error) {
	if s.notifier == nil {
		return "", inport.ErrNotificationsDisabled
	}
	if err := s.notifier.SendTestEmail(); err != nil {
		return "", err
	}
	return s.notifier.Recipient(), nil
}

func (s *Errors) Ping() error { return s.store.Ping() }

func (s *Errors) ListTop(f domain.ListFilter, limit int) ([]domain.ErrorSummary, error) {
	return s.store.ListTop(f, limit)
}

func (s *Errors) ListRecent(f domain.ListFilter, since time.Time) ([]domain.ErrorSummary, error) {
	return s.store.ListRecent(f, since)
}

func (s *Errors) FindByPrefix(prefix string) (string, error) { return s.store.FindByPrefix(prefix) }

func (s *Errors) GetDetail(fp string) (*domain.ErrorDetail, error) { return s.store.GetDetail(fp) }

func (s *Errors) GetTrend(fp string, since time.Time) ([]domain.TrendBucket, error) {
	return s.store.GetTrend(fp, since)
}

func (s *Errors) GetReleases(fp string) ([]domain.ReleaseStats, error) {
	return s.store.GetReleases(fp)
}

func (s *Errors) GetStats() (domain.OverviewStats, error) { return s.store.GetStats() }

func (s *Errors) GCOccurrences(before time.Time) (int64, error) { return s.store.GCOccurrences(before) }

func (s *Errors) ListSilences() ([]domain.SilenceEntry, error) { return s.store.ListSilences() }

func (s *Errors) Silence(fp string, expiresAt *time.Time, reason string) error {
	return s.store.Silence(fp, expiresAt, reason)
}
func (s *Errors) Unsilence(fp string) error { return s.store.Unsilence(fp) }
