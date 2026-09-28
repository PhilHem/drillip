package integrations

import "time"

// CorrelateResult holds the collected context from all configured integrations.
type CorrelateResult struct {
	Logs    []JournalEntry
	Trace   *TraceData
	Metrics *MetricsSnapshot
	Profile []ProfileEntry
}

// Correlate queries all configured external data sources for context around
// an error occurrence. Errors from individual sources are logged and skipped.
func Correlate(cfg Config, occTime time.Time, traceID string) *CorrelateResult {
	result := &CorrelateResult{}

	if cfg.Unit != "" && !occTime.IsZero() {
		if logs, err := QueryJournalctl(cfg.Unit, occTime); err == nil {
			result.Logs = logs
		}
	}

	if cfg.VTURL != "" && traceID != "" {
		if trace, err := QueryVictoriaTraces(cfg.VTURL, traceID); err == nil {
			result.Trace = trace
		}
	}

	if cfg.VMURL != "" && !occTime.IsZero() {
		if metrics, err := QueryVictoriaMetrics(cfg.VMURL, occTime); err == nil {
			result.Metrics = metrics
		}
	}

	if cfg.PyroscopeURL != "" && !occTime.IsZero() {
		if profile, err := QueryPyroscope(cfg.PyroscopeURL, cfg.Service, occTime); err == nil {
			result.Profile = profile
		}
	}

	return result
}
