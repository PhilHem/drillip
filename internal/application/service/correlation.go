package service

import (
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// Correlate queries all configured external data sources for context around
// an error occurrence. Errors from individual sources are skipped.
func (s *Errors) Correlate(occTime time.Time, traceID string) *domain.CorrelateResult {
	result := &domain.CorrelateResult{}
	if s.telemetry == nil {
		return result
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

	return result
}
