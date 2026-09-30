package smtp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/textproto"

	"github.com/PhilHem/drillip/internal/domain"
)

const uncertainDeliveryHint = "Check the recipient mailbox and SMTP server logs before retrying; the message may have been accepted."

// deliveryError records the failed operation while its meaning is still known.
// Network failures take precedence over protocol-stage diagnoses: a lost AUTH
// connection is not evidence that the server rejected the credentials.
func deliveryError(ctx context.Context, stage string, err error) error {
	if err == nil {
		return nil
	}
	code := "smtp_delivery_failed"
	message := "The SMTP send attempt failed."
	hint := uncertainDeliveryHint
	var networkError net.Error
	var response *textproto.Error
	networkFailure := errors.As(err, &networkError) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed)
	switch {
	case stage == "unavailable" || errors.Is(err, context.Canceled) || (networkFailure && errors.Is(ctx.Err(), context.Canceled)):
		code = "notifications_unavailable"
		message = "Email notifications are unavailable."
		hint = "Wait for Drillip to finish restarting, then retry."
	case errors.Is(err, context.DeadlineExceeded) || (networkFailure && errors.Is(ctx.Err(), context.DeadlineExceeded)) ||
		(networkError != nil && networkError.Timeout()):
		code = "smtp_timeout"
		message = "The SMTP server did not respond in time."
		hint = "Check SMTP server availability and network access, then retry."
	case networkFailure || stage == "connect":
		code = "smtp_connection_failed"
		message = "The SMTP connection failed."
		hint = "Check DRILLIP_SMTP_HOST, DRILLIP_SMTP_PORT, and network access from Drillip."
	case stage == "tls":
		code = "smtp_tls_failed"
		message = "SMTP TLS setup failed."
		hint = "Check the STARTTLS endpoint, certificate hostname, and trusted CA bundle; keep certificate verification enabled."
	case stage == "auth":
		code = "smtp_auth_failed"
		message = "SMTP authentication could not be completed."
		hint = "Check SMTP server logs and confirm that the endpoint supports STARTTLS and PLAIN authentication."
		if errors.As(err, &response) && response.Code == 535 {
			code = "smtp_auth_rejected"
			message = "The SMTP server rejected authentication."
			hint = "Check DRILLIP_SMTP_USER, DRILLIP_SMTP_PASS, and the provider's authentication requirements."
		}
	case (stage == "sender" || stage == "recipient") && errors.As(err, &response):
		// These responses reject an address or mailbox. Service availability,
		// resource limits, and command-sequence errors do not establish that.
		switch response.Code {
		case 450, 550, 551, 553:
			if stage == "sender" {
				code = "smtp_sender_rejected"
				message = "The SMTP server rejected the sender."
				hint = "Check DRILLIP_SMTP_FROM and the account's permission to send from that address."
			} else {
				code = "smtp_recipient_rejected"
				message = "The SMTP server rejected the recipient."
				hint = "Check DRILLIP_SMTP_TO and the account's permission to deliver to that address."
			}
		}
	}
	if stage == "completion" {
		// The server may have accepted DATA before its response or QUIT failed.
		// Preserve the cause-specific code without suggesting an immediate retry.
		hint = uncertainDeliveryHint
	}
	return &domain.NotificationError{Code: code, Message: message, Hint: hint, Cause: err}
}
