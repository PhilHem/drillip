package smtp

import outport "github.com/PhilHem/drillip/internal/application/port/out"

var _ outport.Notifier = (*Notifier)(nil)

// Recipient returns the configured notification recipient.
func (n *Notifier) Recipient() string { return n.SMTP.To }
