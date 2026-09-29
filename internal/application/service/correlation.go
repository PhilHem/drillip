package service

import (
	"context"
	"errors"
	"time"

	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/domain"
)

// Correlate resolves the fingerprint, loads the error and selected occurrence,
// and collects available telemetry. Optional sources may fail independently.
func (s *Errors) Correlate(ctx context.Context, query inport.CorrelateQuery) (*domain.Correlation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	fp, err := s.store.FindByPrefix(query.Fingerprint)
	if err != nil {
		return nil, err
	}
	detail, err := s.store.GetCorrelateData(fp)
	if err != nil {
		return nil, err
	}
	result := &domain.Correlation{Error: *detail}
	result.Error.Fingerprint = fp
	occurrence, err := s.store.GetNthOccurrence(fp, query.Nth)
	if errors.Is(err, domain.ErrOccurrenceNotFound) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	occTime, _ := time.Parse(time.RFC3339, occurrence.Timestamp)
	traceID := occurrence.TraceID
	result.Occurrence = &domain.CorrelatedOccurrence{
		Nth:       query.Nth,
		Timestamp: occurrence.Timestamp,
		Time:      occTime,
		TraceID:   traceID,
	}
	if s.telemetry == nil {
		return result, nil
	}

	// A slow optional source must not consume the CLI's entire request deadline.
	enrichment, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !occTime.IsZero() && enrichment.Err() == nil {
		if logs, err := s.telemetry.Logs(enrichment, occTime); err == nil {
			result.Logs = logs
		}
	}

	if traceID != "" && enrichment.Err() == nil {
		if trace, err := s.telemetry.Trace(enrichment, traceID); err == nil {
			result.Trace = trace
		}
	}

	if !occTime.IsZero() && enrichment.Err() == nil {
		if metrics, err := s.telemetry.Metrics(enrichment, occTime); err == nil {
			result.Metrics = metrics
		}
	}

	if !occTime.IsZero() && enrichment.Err() == nil {
		if profile, err := s.telemetry.Profile(enrichment, occTime); err == nil {
			result.Profile = profile
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
