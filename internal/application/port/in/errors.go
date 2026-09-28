// Package inport defines the use cases available to driving adapters.
package inport

import (
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// Queries provides error investigation views.
type Queries interface {
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

// Commands changes error state and retention.
type Commands interface {
	Resolve(string) (domain.ResolveResult, error)
	GCOccurrences(time.Time) (int64, error)
	Silence(string, *time.Time, string) error
	Unsilence(string) error
	ListSilences() ([]domain.SilenceEntry, error)
}

// Errors is the set of error operations exposed through the CLI and JSON API.
type Errors interface {
	Queries
	Commands
}

// Ingestor accepts an event and returns its fingerprint, or "ok" for an ignored event.
type Ingestor interface {
	Ingest(*domain.Event) (string, error)
}

// Health checks whether the application can access its data.
type Health interface{ Ping() error }
