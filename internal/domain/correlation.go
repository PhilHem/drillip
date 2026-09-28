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

// CorrelatedOccurrence is the selected occurrence with its parsed timestamp.
type CorrelatedOccurrence struct {
	Nth       int
	Timestamp string    // stored timestamp, retained for display
	Time      time.Time // zero when the stored timestamp cannot be parsed
	TraceID   string
}

// Correlation combines error details, the selected occurrence, and telemetry.
type Correlation struct {
	Error      CorrelateData
	Occurrence *CorrelatedOccurrence
	Logs       []JournalEntry
	Trace      *TraceData
	Metrics    *MetricsSnapshot
	Profile    []ProfileEntry
}
