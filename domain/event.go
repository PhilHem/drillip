package domain

import "encoding/json"

// Sentry event types

type Event struct {
	EventID     string                     `json:"event_id"`
	Level       string                     `json:"level"`
	Exception   *ExceptionData             `json:"exception"`
	LogEntry    *LogEntry                  `json:"logentry"`
	Message     string                     `json:"message"`
	Request     *RequestData               `json:"request"`
	Breadcrumbs *BreadcrumbData            `json:"breadcrumbs"`
	Contexts    map[string]json.RawMessage `json:"contexts"`
	Release     string                     `json:"release"`
	Environment string                     `json:"environment"`
	User        json.RawMessage            `json:"user"`
	Tags        map[string]string          `json:"tags"`
	Platform    string                     `json:"platform"`
	ServerName  string                     `json:"server_name"`
}

type RequestData struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

// EffectiveLevel returns the event level, defaulting based on type.
func (e *Event) EffectiveLevel() string {
	if e.Level != "" {
		return e.Level
	}
	if e.Exception != nil && len(e.Exception.Values) > 0 {
		return "error"
	}
	return "info"
}

type LogEntry struct {
	Formatted string `json:"formatted"`
	Message   string `json:"message"`
}

// MessageText returns the best available message string.
func (e *Event) MessageText() string {
	if e.LogEntry != nil {
		if e.LogEntry.Formatted != "" {
			return e.LogEntry.Formatted
		}
		return e.LogEntry.Message
	}
	return e.Message
}

// TraceID extracts the trace_id from contexts.trace safely.
func (e *Event) TraceID() string {
	if e.Contexts == nil {
		return ""
	}
	raw, ok := e.Contexts["trace"]
	if !ok {
		return ""
	}
	var tc struct {
		TraceID string `json:"trace_id"`
	}
	if json.Unmarshal(raw, &tc) != nil {
		return ""
	}
	return tc.TraceID
}

type ExceptionData struct {
	Values []ExceptionValue `json:"values"`
}

type ExceptionValue struct {
	Type       string      `json:"type"`
	Value      string      `json:"value"`
	Stacktrace *Stacktrace `json:"stacktrace"`
}

type Stacktrace struct {
	Frames []Frame `json:"frames"`
}

type Frame struct {
	Filename string `json:"filename"`
	Function string `json:"function"`
	Lineno   int    `json:"lineno"`
	Colno    int    `json:"colno"`
	AbsPath  string `json:"abs_path"`
	Module   string `json:"module"`
}

type BreadcrumbData struct {
	Values []json.RawMessage `json:"values"`
}
