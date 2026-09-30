package domain

// NotificationError gives operators a stable diagnosis while retaining the
// underlying delivery error for logs and errors.Is/errors.As.
type NotificationError struct {
	Code    string
	Message string
	Hint    string
	Cause   error
}

func (e *NotificationError) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return e.Message + ": " + e.Cause.Error()
}

func (e *NotificationError) Unwrap() error { return e.Cause }
