// Package service coordinates domain models and application ports.
package service

import (
	"context"
	"errors"
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

func (s *Errors) Resolve(ctx context.Context, prefix string) (domain.ResolveResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ResolveResult{}, err
	}

	fp, err := s.store.FindByPrefix(prefix)
	if err != nil {
		return domain.ResolveResult{}, err
	}
	result, err := s.store.Resolve(fp)
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

func (s *Errors) ListTop(ctx context.Context, f domain.ListFilter, limit int) ([]domain.ErrorSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.store.ListTop(f, limit)
}

func (s *Errors) ListRecent(ctx context.Context, f domain.ListFilter, since time.Time) ([]domain.ErrorSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.store.ListRecent(f, since)
}

func (s *Errors) GetDetail(ctx context.Context, reference string) (*domain.ErrorDetail, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	fp, err := s.store.FindByPrefix(reference)
	if err != nil {
		return nil, err
	}
	return s.store.GetDetail(fp)
}

func (s *Errors) GetTrend(ctx context.Context, reference string, since time.Time) (domain.Trend, error) {
	if err := ctx.Err(); err != nil {
		return domain.Trend{}, err
	}

	fp, err := s.store.FindByPrefix(reference)
	if err != nil {
		return domain.Trend{}, err
	}
	buckets, err := s.store.GetTrend(fp, since)
	if err != nil {
		return domain.Trend{}, err
	}
	return domain.Trend{Fingerprint: fp, Buckets: buckets}, nil
}

func (s *Errors) GetReleases(ctx context.Context, reference string) (domain.Releases, error) {
	if err := ctx.Err(); err != nil {
		return domain.Releases{}, err
	}

	fp, err := s.store.FindByPrefix(reference)
	if err != nil {
		return domain.Releases{}, err
	}
	releases, err := s.store.GetReleases(fp)
	if err != nil {
		return domain.Releases{}, err
	}
	return domain.Releases{Fingerprint: fp, Releases: releases}, nil
}

func (s *Errors) GetStats(ctx context.Context) (domain.OverviewStats, error) {
	if err := ctx.Err(); err != nil {
		return domain.OverviewStats{}, err
	}

	return s.store.GetStats()
}

func (s *Errors) GCOccurrences(ctx context.Context, before time.Time) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	return s.store.GCOccurrences(before)
}

func (s *Errors) ListSilences(ctx context.Context) ([]domain.SilenceEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.store.ListSilences()
}

func (s *Errors) Silence(ctx context.Context, reference string, expiresAt *time.Time, reason string) (domain.SilenceResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.SilenceResult{}, err
	}

	fp, err := s.store.FindByPrefix(reference)
	if err != nil {
		return domain.SilenceResult{}, err
	}
	if expiresAt != nil {
		applied := expiresAt.UTC().Truncate(time.Second)
		expiresAt = &applied
	}
	if err := s.store.Silence(fp, expiresAt, reason); err != nil {
		return domain.SilenceResult{}, err
	}
	return domain.SilenceResult{Fingerprint: fp, ExpiresAt: expiresAt}, nil
}
func (s *Errors) Unsilence(ctx context.Context, reference string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	fp, err := s.store.FindByPrefix(reference)
	// Older versions permitted silence rules without a corresponding error.
	// Keep exact deletion of those rules available during migration.
	if errors.Is(err, domain.ErrErrorNotFound) {
		entries, listErr := s.store.ListSilences()
		if listErr != nil {
			return "", listErr
		}
		for _, entry := range entries {
			if entry.Fingerprint == reference {
				return reference, s.store.Unsilence(reference)
			}
		}
	}
	if err != nil {
		return "", err
	}
	return fp, s.store.Unsilence(fp)
}
