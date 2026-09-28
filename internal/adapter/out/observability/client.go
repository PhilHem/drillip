package observability

import (
	"time"

	outport "github.com/PhilHem/drillip/internal/application/port/out"
	"github.com/PhilHem/drillip/internal/domain"
)

// Client reads configured telemetry sources.
type Client struct{ Config Config }

var _ outport.Telemetry = Client{}

func (c Client) Logs(at time.Time) ([]domain.JournalEntry, error) {
	return QueryJournalctl(c.Config.Unit, at)
}
func (c Client) Trace(id string) (*domain.TraceData, error) {
	return QueryVictoriaTraces(c.Config.VTURL, id)
}
func (c Client) Metrics(at time.Time) (*domain.MetricsSnapshot, error) {
	return QueryVictoriaMetrics(c.Config.VMURL, at)
}
func (c Client) Profile(at time.Time) ([]domain.ProfileEntry, error) {
	return QueryPyroscope(c.Config.PyroscopeURL, c.Config.Service, at)
}
