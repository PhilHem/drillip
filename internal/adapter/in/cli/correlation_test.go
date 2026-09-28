package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/domain"
)

type correlatorFunc func(inport.CorrelateQuery) (*domain.Correlation, error)

func (f correlatorFunc) Correlate(query inport.CorrelateQuery) (*domain.Correlation, error) {
	return f(query)
}

func TestCorrelateCommandNeedsOnlyCorrelationPort(t *testing.T) {
	calls := 0
	// Leave Errors nil: the command only formats the complete use-case result.
	c := &CLI{Correlation: correlatorFunc(func(query inport.CorrelateQuery) (*domain.Correlation, error) {
		calls++
		if query.Fingerprint != "abcd" || query.Nth != 2 {
			t.Fatalf("query = %+v", query)
		}
		return &domain.Correlation{
			Error: domain.CorrelateData{Fingerprint: "abcdef0123456789", Type: "CheckoutError", Value: "failed"},
			Occurrence: &domain.CorrelatedOccurrence{
				Nth: 2, Timestamp: "2026-09-28T12:00:00Z", Time: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
			},
			Logs: []domain.JournalEntry{{Message: "diagnostic context"}},
		}, nil
	})}
	var output bytes.Buffer
	c.RunCorrelate([]string{"--nth", "2", "abcd"}, &output)
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	for _, expected := range []string{"CheckoutError", "abcdef0123456789", "#2 at 2026-09-28T12:00:00Z", "diagnostic context", "drillip show abcdef01"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("missing %q in output: %s", expected, &output)
		}
	}
}

func TestCorrelateCommandPreservesPartialResultsAndErrors(t *testing.T) {
	for _, failed := range []bool{false, true} {
		c := &CLI{Correlation: correlatorFunc(func(inport.CorrelateQuery) (*domain.Correlation, error) {
			if failed {
				return nil, errors.New("error unavailable")
			}
			return &domain.Correlation{Error: domain.CorrelateData{Fingerprint: "abcdef0123456789", Type: "CheckoutError"}}, nil
		})}
		var output bytes.Buffer
		c.RunCorrelate([]string{"abcd"}, &output)
		if failed {
			if output.String() != "error not found: abcd\n" {
				t.Fatalf("output = %q", output.String())
			}
			continue
		}
		if !strings.Contains(output.String(), "CheckoutError") || strings.Contains(output.String(), "Occurrence:") {
			t.Fatalf("partial output = %s", &output)
		}
	}
}
