package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/domain"
)

type testEmailFunc func() (string, error)

func (f testEmailFunc) SendTestEmail() (string, error) { return f() }

func TestTestEmailDiagnostics(t *testing.T) {
	diagnosis := &domain.NotificationError{
		Code:    "smtp_auth_rejected",
		Message: "The SMTP server rejected the login.",
		Hint:    "Check DRILLIP_SMTP_USER and DRILLIP_SMTP_PASS.",
		Cause:   errors.New("535 upstream diagnostic with private details"),
	}
	for _, tc := range []struct {
		name string
		err  error
		want map[string]string
	}{
		{
			name: "typed failure through wrapping",
			err:  fmt.Errorf("notification attempt: %w", diagnosis),
			want: map[string]string{"error": diagnosis.Message, "code": diagnosis.Code, "hint": diagnosis.Hint},
		},
		{
			name: "unknown failure",
			err:  errors.New("upstream diagnostic with private details"),
			want: map[string]string{
				"error": "The SMTP send attempt failed.",
				"code":  "smtp_delivery_failed",
				"hint":  "Check the recipient mailbox and SMTP server logs before retrying; the message may have been accepted.",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := &Handler{Notifications: testEmailFunc(func() (string, error) {
				calls++
				return "", tc.err
			})}
			w := httptest.NewRecorder()
			h.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/0/test-email/", nil))
			if calls != 1 || w.Code != http.StatusBadGateway {
				t.Fatalf("calls=%d status=%d body=%s", calls, w.Code, w.Body)
			}
			if w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("content type = %q", w.Header().Get("Content-Type"))
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body) != len(tc.want) {
				t.Fatalf("unexpected fields: %v", body)
			}
			for key, want := range tc.want {
				if body[key] != want {
					t.Errorf("%s = %q, want %q", key, body[key], want)
				}
			}
			// Existing consumers can still decode the original string field.
			var legacy struct{ Error string }
			if err := json.Unmarshal(w.Body.Bytes(), &legacy); err != nil || legacy.Error == "" {
				t.Fatalf("legacy decoding: %+v, %v", legacy, err)
			}
			if strings.Contains(w.Body.String(), "private details") {
				t.Fatal("raw cause escaped the diagnostic boundary")
			}
		})
	}
}

func TestTestEmailDisabledDiagnostics(t *testing.T) {
	for _, notifications := range []inport.Notifications{
		nil,
		testEmailFunc(func() (string, error) { return "", fmt.Errorf("disabled: %w", inport.ErrNotificationsDisabled) }),
	} {
		w := httptest.NewRecorder()
		(&Handler{Notifications: notifications}).HandleTestEmail(w, httptest.NewRequest(http.MethodPost, "/api/0/test-email/", nil))
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusServiceUnavailable || body["error"] != "notifications not configured" || body["code"] != "notifications_not_configured" {
			t.Fatalf("status=%d body=%v", w.Code, body)
		}
		for _, setting := range []string{"DRILLIP_SMTP_HOST", "DRILLIP_SMTP_TO"} {
			if !strings.Contains(body["hint"], setting) {
				t.Errorf("hint missing %s: %v", setting, body)
			}
		}
	}
}

func TestTestEmailSuccessAndMethodContract(t *testing.T) {
	calls := 0
	h := &Handler{Notifications: testEmailFunc(func() (string, error) {
		calls++
		return "ops@example.com", nil
	})}
	w := httptest.NewRecorder()
	h.HandleTestEmail(w, httptest.NewRequest(http.MethodPost, "/api/0/test-email/", nil))
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || len(body) != 2 || body["status"] != "sent" || body["to"] != "ops@example.com" || calls != 1 {
		t.Fatalf("status=%d body=%v calls=%d", w.Code, body, calls)
	}
	w = httptest.NewRecorder()
	h.HandleTestEmail(w, httptest.NewRequest(http.MethodGet, "/api/0/test-email/", nil))
	if w.Code != http.StatusMethodNotAllowed || calls != 1 {
		t.Fatalf("status=%d calls=%d", w.Code, calls)
	}
}
