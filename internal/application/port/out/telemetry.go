package outport

import (
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// Telemetry reads diagnostic context from configured external sources.
// Disabled sources return empty results without an error.
type Telemetry interface {
	Logs(time.Time) ([]domain.JournalEntry, error)
	Trace(string) (*domain.TraceData, error)
	Metrics(time.Time) (*domain.MetricsSnapshot, error)
	Profile(time.Time) ([]domain.ProfileEntry, error)
}
