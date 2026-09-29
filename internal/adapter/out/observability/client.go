package observability

import (
	"context"
	"time"

	outport "github.com/PhilHem/drillip/internal/application/port/out"
	"github.com/PhilHem/drillip/internal/domain"
)

// Client reads configured telemetry sources.
type Client struct{ Config Config }

var _ outport.Telemetry = Client{}

func (c Client) Logs(ctx context.Context, at time.Time) ([]domain.JournalEntry, error) {
	return QueryJournalctl(ctx, c.Config.Unit, at)
}
func (c Client) Trace(ctx context.Context, id string) (*domain.TraceData, error) {
	return QueryVictoriaTraces(ctx, c.Config.VTURL, id)
}
func (c Client) Metrics(ctx context.Context, at time.Time) (*domain.MetricsSnapshot, error) {
	return QueryVictoriaMetrics(ctx, c.Config.VMURL, at)
}
func (c Client) Profile(ctx context.Context, at time.Time) ([]domain.ProfileEntry, error) {
	return QueryPyroscope(ctx, c.Config.PyroscopeURL, c.Config.Service, at)
}
