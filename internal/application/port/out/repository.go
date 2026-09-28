// Package outport defines the dependencies required by application services.
package outport

import (
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// EventStore persists ingested events and checks notification silences.
type EventStore interface {
	StoreEvent(*domain.Event) (domain.StoreResult, error)
	IsSilenced(string) bool
}

// QueryStore loads error investigation data.
type QueryStore interface {
	ListTop(domain.ListFilter, int) ([]domain.ErrorSummary, error)
	ListRecent(domain.ListFilter, time.Time) ([]domain.ErrorSummary, error)
	FindByPrefix(string) (string, error)
	GetDetail(string) (*domain.ErrorDetail, error)
	GetTrend(string, time.Time) ([]domain.TrendBucket, error)
	GetReleases(string) ([]domain.ReleaseStats, error)
	GetStats() (domain.OverviewStats, error)
	GetCorrelateData(string) (*domain.CorrelateData, error)
	GetNthOccurrence(string, int) (*domain.Occurrence, error)
}

// StateStore persists manual state and retention changes.
type StateStore interface {
	Resolve(string) (domain.ResolveResult, error)
	GCOccurrences(time.Time) (int64, error)
	Silence(string, *time.Time, string) error
	Unsilence(string) error
	ListSilences() ([]domain.SilenceEntry, error)
}

// MaintenanceStore supports periodic housekeeping.
type MaintenanceStore interface {
	AutoResolve(time.Duration) ([]domain.ResolvedError, error)
	PruneExpiredSilences() (int64, error)
	GCOccurrences(time.Time) (int64, error)
}

// Repository supplies persistence operations for the error service.
// Connection setup, shutdown, SQL handles, and migrations are adapter concerns.
type Repository interface {
	EventStore
	QueryStore
	StateStore
	Ping() error
}
