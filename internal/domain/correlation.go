package domain

import "time"

type JournalEntry struct {
	Timestamp string `json:"__REALTIME_TIMESTAMP"`
	Message   string `json:"MESSAGE"`
	Priority  string `json:"PRIORITY"`
}

type TraceData struct {
	ServiceName string
	Spans       []TraceSpan
}

type TraceSpan struct {
	OperationName string
	Duration      time.Duration
	Tags          map[string]string
}

type MetricsSnapshot struct {
	Values map[string]string
}

type ProfileEntry struct {
	Function string
	Self     int64
}

// CorrelateResult holds the collected context from all configured integrations.
type CorrelateResult struct {
	Logs    []JournalEntry
	Trace   *TraceData
	Metrics *MetricsSnapshot
	Profile []ProfileEntry
}
