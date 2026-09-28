package inport

import (
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// Correlator collects diagnostic context around an occurrence.
type Correlator interface {
	Correlate(time.Time, string) *domain.CorrelateResult
}
