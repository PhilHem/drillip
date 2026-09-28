package inport

import "github.com/PhilHem/drillip/internal/domain"

// CorrelateQuery identifies an error and the occurrence to investigate.
type CorrelateQuery struct {
	Fingerprint string // full fingerprint or prefix
	Nth         int    // occurrence index, starting at 1 for the most recent
}

// Correlator resolves an error and collects its diagnostic context.
// Error lookup failures are returned. An unavailable occurrence or telemetry
// source leaves that part of the result empty without losing the error data.
type Correlator interface {
	Correlate(CorrelateQuery) (*domain.Correlation, error)
}
