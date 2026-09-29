package domain

import (
	"errors"
	"time"
)

var (
	ErrOccurrenceNotFound   = errors.New("occurrence not found")
	ErrErrorNotFound        = errors.New("error not found")
	ErrAmbiguousFingerprint = errors.New("ambiguous fingerprint")
	ErrInvalidFingerprint   = errors.New("invalid fingerprint")
)

// ListFilter holds optional filters for error list queries.
type ListFilter struct {
	Level  string
	TagKey string
	TagVal string
}

// ErrorSummary is a compact error representation for list views.
type ErrorSummary struct {
	Fingerprint string
	Count       int
	Level       string
	Type        string
	Value       string
	FirstSeen   string
	LastSeen    string
	ResolvedAt  string
	State       string
}

// ErrorDetail is the full representation of a stored error.
type ErrorDetail struct {
	Fingerprint string
	Count       int
	Level       string
	Type        string
	Value       string
	Release     string
	Environment string
	Platform    string
	FirstSeen   string
	LastSeen    string
	ResolvedAt  string
	State       string
	Stacktrace  string
	Breadcrumbs string
	UserContext string
	Tags        string
	TagDist     map[string]TagDist
}

// TrendBucket holds the occurrence count for a single hour.
type TrendBucket struct {
	Hour  string
	Count int
}

// ReleaseStats holds occurrence counts grouped by release.
type ReleaseStats struct {
	Release   string
	Count     int
	FirstSeen string
	LastSeen  string
}

// OverviewStats holds aggregate database statistics.
type OverviewStats struct {
	UniqueErrors     int
	TotalOccurrences int
	FirstSeen        string
	LastSeen         string
}

// CorrelateData holds error data needed for the correlate view.
type CorrelateData struct {
	Fingerprint string
	Type        string
	Value       string
	Stacktrace  string
	Breadcrumbs string
	UserContext string
}

// Occurrence holds a single occurrence's timestamp and trace ID.
type Occurrence struct {
	Timestamp string
	TraceID   string
}

// StoreResult holds the outcome of storing an event.
type StoreResult struct {
	Fingerprint      string
	IsNew            bool
	IsRegression     bool          // was resolved, now reappeared
	ResolvedDuration time.Duration // how long it was resolved before regressing
}

// SilenceEntry represents an active silence rule.
type SilenceEntry struct {
	Fingerprint string
	CreatedAt   string
	ExpiresAt   string // empty if permanent
	Reason      string
}

// ResolveResult holds the outcome of a manual resolve operation.
type ResolveResult struct {
	Matched     int64
	Fingerprint string // full fingerprint of the matched error
	ResolvedAt  string
	Resolved    []ResolvedError // details of resolved errors
}

// TagValue holds a single tag value and its occurrence count/percentage.
type TagValue struct {
	Value   string `json:"value"`
	Count   int    `json:"count"`
	Percent int    `json:"percent"`
}

// TagDist holds the distribution of values for a single tag key.
type TagDist struct {
	Values []TagValue `json:"values"`
}

// Trend is an occurrence histogram for one uniquely identified error.
type Trend struct {
	Fingerprint string
	Buckets     []TrendBucket
}

// Releases is the retained release history for one uniquely identified error.
type Releases struct {
	Fingerprint string
	Releases    []ReleaseStats
}

// SilenceResult reports the canonical error and the expiry actually stored.
// SQLite timestamps have whole-second precision; nil means permanent.
type SilenceResult struct {
	Fingerprint string
	ExpiresAt   *time.Time
}
