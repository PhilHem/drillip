package outport

import (
	"context"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// Telemetry reads diagnostic context from configured external sources.
// Implementations must stop subprocesses and requests when ctx is cancelled.
// Disabled sources return empty results without an error.
type Telemetry interface {
	Logs(context.Context, time.Time) ([]domain.JournalEntry, error)
	Trace(context.Context, string) (*domain.TraceData, error)
	Metrics(context.Context, time.Time) (*domain.MetricsSnapshot, error)
	Profile(context.Context, time.Time) ([]domain.ProfileEntry, error)
}
