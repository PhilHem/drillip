package inport

import "errors"

// ErrNotificationsDisabled means no notification channel is configured.
var ErrNotificationsDisabled = errors.New("notifications not configured")

// Notifications sends a test message and returns its recipient.
type Notifications interface{ SendTestEmail() (string, error) }
