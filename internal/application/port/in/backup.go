package inport

import (
	"context"

	"github.com/PhilHem/drillip/internal/domain"
)

// Backups creates a consistent snapshot without stopping the running server.
type Backups interface {
	Backup(context.Context) (domain.DatabaseBackup, error)
}
