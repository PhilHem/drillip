package httpwire

import (
	"encoding/json"
	"github.com/PhilHem/drillip/internal/domain"
)

type Error struct {
	Fingerprint string `json:"fingerprint"`
	Count       int    `json:"count"`
	Level       string `json:"level"`
	Type        string `json:"type"`
	Value       string `json:"value"`
	FirstSeen   string `json:"first_seen"`
	LastSeen    string `json:"last_seen"`
	ResolvedAt  string `json:"resolved_at,omitempty"`
	State       string `json:"state"`
}

type ErrorDetail struct {
	Fingerprint string                    `json:"fingerprint"`
	Count       int                       `json:"count"`
	Level       string                    `json:"level"`
	Type        string                    `json:"type"`
	Value       string                    `json:"value"`
	Release     string                    `json:"release,omitempty"`
	Environment string                    `json:"environment,omitempty"`
	Platform    string                    `json:"platform,omitempty"`
	FirstSeen   string                    `json:"first_seen"`
	LastSeen    string                    `json:"last_seen"`
	ResolvedAt  string                    `json:"resolved_at,omitempty"`
	State       string                    `json:"state"`
	Stacktrace  json.RawMessage           `json:"stacktrace,omitempty"`
	Breadcrumbs json.RawMessage           `json:"breadcrumbs,omitempty"`
	User        json.RawMessage           `json:"user,omitempty"`
	Tags        json.RawMessage           `json:"tags,omitempty"`
	TagDist     map[string]domain.TagDist `json:"tag_distribution,omitempty"`
}

type Stats struct {
	UniqueErrors     int    `json:"unique_errors"`
	TotalOccurrences int    `json:"total_occurrences"`
	FirstSeen        string `json:"first_seen,omitempty"`
	LastSeen         string `json:"last_seen,omitempty"`
}

type Bucket struct {
	Hour  string `json:"hour"`
	Count int    `json:"count"`
}

type Release struct {
	Release   string `json:"release"`
	Count     int    `json:"count"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
}

type GCResult struct {
	Deleted   int64  `json:"deleted"`
	Threshold string `json:"threshold"`
}

type Silence struct {
	Fingerprint string `json:"fingerprint"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type Correlation struct {
	Fingerprint string            `json:"fingerprint"`
	Type        string            `json:"type"`
	Value       string            `json:"value"`
	Occurrence  *Occurrence       `json:"occurrence,omitempty"`
	Stacktrace  json.RawMessage   `json:"stacktrace,omitempty"`
	Breadcrumbs json.RawMessage   `json:"breadcrumbs,omitempty"`
	User        json.RawMessage   `json:"user,omitempty"`
	Logs        []LogEntry        `json:"logs,omitempty"`
	Trace       *TraceData        `json:"trace,omitempty"`
	Metrics     map[string]string `json:"metrics,omitempty"`
	Profile     []ProfileEntry    `json:"profile,omitempty"`
}

type Occurrence struct {
	Nth       int    `json:"nth"`
	Timestamp string `json:"timestamp"`
	TraceID   string `json:"trace_id,omitempty"`
}

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	Priority  string `json:"priority,omitempty"`
}

type TraceData struct {
	ServiceName string      `json:"service_name"`
	Spans       []TraceSpan `json:"spans"`
}

type TraceSpan struct {
	OperationName string `json:"operation_name"`
	Duration      string `json:"duration"`
}

type ProfileEntry struct {
	Function string `json:"function"`
}

// FromSummary converts a domain.ErrorSummary to the API response type.
func FromSummary(s domain.ErrorSummary) Error {
	return Error{
		Fingerprint: s.Fingerprint,
		Count:       s.Count,
		Level:       s.Level,
		Type:        s.Type,
		Value:       s.Value,
		FirstSeen:   s.FirstSeen,
		LastSeen:    s.LastSeen,
		ResolvedAt:  s.ResolvedAt,
		State:       s.State,
	}
}

// FromDetail converts a domain.ErrorDetail to the API response type.
func FromDetail(d *domain.ErrorDetail) ErrorDetail {
	ad := ErrorDetail{
		Fingerprint: d.Fingerprint,
		Count:       d.Count,
		Level:       d.Level,
		Type:        d.Type,
		Value:       d.Value,
		Release:     d.Release,
		Environment: d.Environment,
		Platform:    d.Platform,
		FirstSeen:   d.FirstSeen,
		LastSeen:    d.LastSeen,
		ResolvedAt:  d.ResolvedAt,
		State:       d.State,
		TagDist:     d.TagDist,
	}
	if d.Stacktrace != "" {
		ad.Stacktrace = json.RawMessage(d.Stacktrace)
	}
	if d.Breadcrumbs != "" {
		ad.Breadcrumbs = json.RawMessage(d.Breadcrumbs)
	}
	if d.UserContext != "" && d.UserContext != "null" {
		ad.User = json.RawMessage(d.UserContext)
	}
	if d.Tags != "" && d.Tags != "null" {
		ad.Tags = json.RawMessage(d.Tags)
	}
	return ad
}
