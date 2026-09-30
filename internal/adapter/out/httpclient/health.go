package httpclient

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/PhilHem/drillip/internal/adapter/httpwire"
	"github.com/PhilHem/drillip/internal/domain"
)

func (c *Client) DatabaseHistory(ctx context.Context) (domain.DatabaseHistory, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	capabilities, err := c.capabilities(ctx)
	if err != nil {
		return domain.DatabaseHistory{}, err
	}
	if !slices.Contains(capabilities.Features, httpwire.FeatureDatabaseHistory) {
		return domain.DatabaseHistory{}, fmt.Errorf("server does not support health --details; upgrade the server together with the CLI")
	}
	var details httpwire.HealthDetails
	if err := c.exchange(ctx, http.MethodGet, "/api/0/health/", nil, &details, "status"); err != nil {
		return domain.DatabaseHistory{}, err
	}
	if details.Status != "ok" {
		return domain.DatabaseHistory{}, fmt.Errorf("invalid database health status %q", details.Status)
	}
	return domain.DatabaseHistory{LastBackupGeneratedAt: details.LastBackupGeneratedAt,
		LastRestoredAt: details.LastRestoredAt, RestoredSnapshotAt: details.RestoredSnapshotAt}, nil
}
