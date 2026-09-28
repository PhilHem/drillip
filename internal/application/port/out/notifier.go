package outport

import (
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// ResolutionNotifier delivers resolved-error notifications.
type ResolutionNotifier interface {
	NotifyResolved([]domain.ResolvedError)
}

// Notifier delivers application notifications through a configured channel.
type Notifier interface {
	ResolutionNotifier
	NotifyNewError(*domain.Event, string, bool, time.Duration)
	SendTestEmail() error
	Recipient() string
}
