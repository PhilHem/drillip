// Package inport defines the use cases available to driving adapters.
package inport

import (
	"context"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// Queries provides complete error investigation views. Reference arguments accept
// a full fingerprint or unique prefix; unknown, invalid, and ambiguous references
// fail before the view is queried. Results carry the canonical fingerprint.
type Queries interface {
	List(context.Context, domain.ListQuery) (domain.ErrorPage, error)
	ListTop(context.Context, domain.ListFilter, int) ([]domain.ErrorSummary, error)
	ListRecent(context.Context, domain.ListFilter, time.Time) ([]domain.ErrorSummary, error)
	GetDetail(context.Context, string) (*domain.ErrorDetail, error)
	GetTrend(context.Context, string, time.Time) (domain.Trend, error)
	GetReleases(context.Context, string) (domain.Releases, error)
	GetStats(context.Context) (domain.OverviewStats, error)
}

// Commands changes error state and retention.
type Commands interface {
	Resolve(context.Context, string) (domain.ResolveResult, error)
	GCOccurrences(context.Context, time.Time) (int64, error)
	Silence(context.Context, string, *time.Time, string) (domain.SilenceResult, error)
	Unsilence(context.Context, string) (string, error)
	ListSilences(context.Context) ([]domain.SilenceEntry, error)
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
