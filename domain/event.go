package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

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

// validLevels contains the Sentry-recognized severity levels.
var validLevels = map[string]bool{
	"fatal":   true,
	"error":   true,
	"warning": true,
	"info":    true,
	"debug":   true,
}

// stripCRLF removes carriage return and newline characters from strings
// to prevent header injection when values are used in email subjects or logs.
func stripCRLF(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

// Sanitize clamps oversized fields in-place so the event is safe to store.
// It truncates rather than rejecting — Sentry SDKs send real data and we
// should accept it gracefully.
func (e *Event) Sanitize() {
	if len(e.Message) > 10000 {
		e.Message = e.Message[:10000]
	}

	if e.Level != "" && !validLevels[e.Level] {
		e.Level = ""
	}

	if e.Exception != nil {
		if len(e.Exception.Values) > 10 {
			e.Exception.Values = e.Exception.Values[:10]
		}
		for i := range e.Exception.Values {
			e.Exception.Values[i].Type = stripCRLF(e.Exception.Values[i].Type)
			e.Exception.Values[i].Value = stripCRLF(e.Exception.Values[i].Value)
			if len(e.Exception.Values[i].Type) > 200 {
				e.Exception.Values[i].Type = e.Exception.Values[i].Type[:200]
			}
			if len(e.Exception.Values[i].Value) > 5000 {
				e.Exception.Values[i].Value = e.Exception.Values[i].Value[:5000]
			}
			st := e.Exception.Values[i].Stacktrace
			if st != nil {
				if len(st.Frames) > 100 {
					st.Frames = st.Frames[:100]
				}
				for j := range st.Frames {
					if len(st.Frames[j].Filename) > 500 {
						st.Frames[j].Filename = st.Frames[j].Filename[:500]
					}
					if len(st.Frames[j].Function) > 200 {
						st.Frames[j].Function = st.Frames[j].Function[:200]
					}
					if len(st.Frames[j].AbsPath) > 500 {
						st.Frames[j].AbsPath = st.Frames[j].AbsPath[:500]
					}
					if len(st.Frames[j].Module) > 200 {
						st.Frames[j].Module = st.Frames[j].Module[:200]
					}
				}
			}
		}
	}

	if e.LogEntry != nil {
		if len(e.LogEntry.Formatted) > 10000 {
			e.LogEntry.Formatted = e.LogEntry.Formatted[:10000]
		}
		if len(e.LogEntry.Message) > 10000 {
			e.LogEntry.Message = e.LogEntry.Message[:10000]
		}
	}

	if e.Breadcrumbs != nil && len(e.Breadcrumbs.Values) > 100 {
		e.Breadcrumbs.Values = e.Breadcrumbs.Values[len(e.Breadcrumbs.Values)-100:]
	}

	if len(e.Tags) > 50 {
		kept := 0
		for k := range e.Tags {
			if kept >= 50 {
				delete(e.Tags, k)
			} else {
				kept++
			}
		}
	}
	for k, v := range e.Tags {
		if len(v) > 200 {
			e.Tags[k] = v[:200]
		}
	}

	if e.Request != nil && len(e.Request.URL) > 2000 {
		e.Request.URL = e.Request.URL[:2000]
	}

	if len(e.Environment) > 100 {
		e.Environment = e.Environment[:100]
	}
	if len(e.Release) > 200 {
		e.Release = e.Release[:200]
	}
	if len(e.ServerName) > 200 {
		e.ServerName = e.ServerName[:200]
	}
	if len(e.Platform) > 50 {
		e.Platform = e.Platform[:50]
	}
}

// ValidFingerprint reports whether fp is a valid hex fingerprint
// (1–16 lowercase hex characters).
func ValidFingerprint(fp string) bool {
	if len(fp) == 0 || len(fp) > 16 {
		return false
	}
	for _, c := range fp {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// DeriveState returns "resolved", "new", or "ongoing" based on resolved_at and first_seen.
func DeriveState(resolvedAt, firstSeen string) string {
	if resolvedAt != "" {
		return "resolved"
	}
	if t, err := time.Parse(time.RFC3339, firstSeen); err == nil {
		if time.Since(t) < time.Hour {
			return "new"
		}
	}
	return "ongoing"
}

// ParseDuration parses duration strings like "24h", "7d", "1w".
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	suffix := s[len(s)-1]
	numStr := s[:len(s)-1]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	switch suffix {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown suffix %q (use h/d/w)", string(suffix))
	}
}

// ParseTag splits "key=value" into (key, value, true) or ("", "", false).
func ParseTag(s string) (string, string, bool) {
	i := strings.IndexByte(s, '=')
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}
