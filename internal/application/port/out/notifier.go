package outport

import (
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// ResolutionNotifier accepts resolved-error notifications before returning.
// Implementations own asynchronous delivery and its shutdown lifecycle.
type ResolutionNotifier interface {
	NotifyResolved([]domain.ResolvedError)
}

// Notifier accepts application notifications before returning.
// Delivery is asynchronous; SendTestEmail waits for its result.
type Notifier interface {
	ResolutionNotifier
	NotifyNewError(*domain.Event, string, bool, time.Duration)
	SendTestEmail() error
	Recipient() string
}
