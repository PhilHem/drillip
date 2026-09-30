package inport

import (
	"context"

	"github.com/PhilHem/drillip/internal/domain"
)

// DatabaseStatus reads the current database's persisted operation history.
type DatabaseStatus interface {
	DatabaseHistory(context.Context) (domain.DatabaseHistory, error)
}

// ServerHealth supports a cheap probe and an explicit request for details.
type ServerHealth interface {
	Health(context.Context) error
	DatabaseStatus
}
