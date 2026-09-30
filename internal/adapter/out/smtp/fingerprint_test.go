package smtp

import (
	"regexp"
	"strings"
	"testing"

	"github.com/PhilHem/drillip/internal/domain"
)

func TestEmailFingerprintsRemainActionableWhenPrefixesCollide(t *testing.T) {
	fingerprints := []string{"c2a8398a3347b02d", "c2a8398a15ea5f75"}
	event := &domain.Event{Message: "checkout failed", Level: "error"}
	items := []pendingNotification{
		{Event: event, Fingerprint: fingerprints[0]},
		{Event: event, Fingerprint: fingerprints[1], IsRegression: true},
	}
	resolved := []domain.ResolvedError{
		{Fingerprint: fingerprints[0], Type: "message", Value: event.Message},
		{Fingerprint: fingerprints[1], Type: "message", Value: event.Message},
	}
	tests := []struct {
		name string
		body string
	}{
		{"digest HTML", formatDigestHTMLEmail(items, "checkout")},
		{"digest plain", formatDigestPlainEmail(items)},
		{"resolved HTML", formatResolvedHTMLEmail(resolved, "checkout")},
		{"resolved plain", formatResolvedPlainEmail(resolved, "checkout")},
	}
	for _, fp := range fingerprints {
		for _, format := range []struct {
			name string
			body string
		}{
			{"individual HTML " + fp, formatHTMLEmail(event, fp, "checkout", false, 0)},
			{"individual plain " + fp, formatPlainEmail(event, fp, "checkout", false, 0)},
		} {
			t.Run(format.name, func(t *testing.T) {
				// Match the entire argument, so a correct fingerprint elsewhere
				// in the message cannot hide a broken copyable command.
				commands := regexp.MustCompile(`drillip (show|correlate) ([0-9a-f]+)`).FindAllStringSubmatch(format.body, -1)
				if len(commands) != 2 {
					t.Fatalf("expected two investigation commands, got %v", commands)
				}
				for _, command := range commands {
					if command[2] != fp {
						t.Errorf("copyable command %q does not identify %s", command[0], fp)
					}
				}
			})
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, fp := range fingerprints {
				if !strings.Contains(tt.body, fp) {
					t.Errorf("missing full reference %s", fp)
				}
			}
			if regexp.MustCompile(`\bc2a8398a\b`).MatchString(tt.body) {
				t.Error("email still presents the ambiguous shared prefix as a reference")
			}
		})
	}
}
