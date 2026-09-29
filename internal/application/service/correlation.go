package service

import (
	"context"
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
	if err != nil {
		return result, nil
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

	if !occTime.IsZero() {
		if logs, err := s.telemetry.Logs(occTime); err == nil {
			result.Logs = logs
		}
	}

	if traceID != "" {
		if trace, err := s.telemetry.Trace(traceID); err == nil {
			result.Trace = trace
		}
	}

	if !occTime.IsZero() {
		if metrics, err := s.telemetry.Metrics(occTime); err == nil {
			result.Metrics = metrics
		}
	}

	if !occTime.IsZero() {
		if profile, err := s.telemetry.Profile(occTime); err == nil {
			result.Profile = profile
		}
	}

	return result, nil
}
