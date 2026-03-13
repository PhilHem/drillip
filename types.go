package main

import "encoding/json"

// Sentry event types

type Event struct {
	EventID     string                     `json:"event_id"`
	Exception   *ExceptionData             `json:"exception"`
	Breadcrumbs *BreadcrumbData            `json:"breadcrumbs"`
	Contexts    map[string]json.RawMessage `json:"contexts"`
	Release     string                     `json:"release"`
	Environment string                     `json:"environment"`
	User        json.RawMessage            `json:"user"`
	Tags        map[string]string          `json:"tags"`
	Platform    string                     `json:"platform"`
	ServerName  string                     `json:"server_name"`
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

type EnvelopeItemHeader struct {
	Type   string `json:"type"`
	Length int    `json:"length"`
}
