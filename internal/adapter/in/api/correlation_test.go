package api

import (
	"context"
	"encoding/json"
	"github.com/PhilHem/drillip/internal/adapter/httpwire"
	"net/http"
	"net/http/httptest"
	"testing"

	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/domain"
)

type correlatorFunc func(inport.CorrelateQuery) (*domain.Correlation, error)

func (f correlatorFunc) Correlate(_ context.Context, query inport.CorrelateQuery) (*domain.Correlation, error) {
	return f(query)
}

func TestCorrelateHandlerNeedsOnlyCorrelationPort(t *testing.T) {
	for _, tc := range []struct {
		parameter string
		nth       int
	}{
		{"", 1}, {"2", 2}, {"0", 1}, {"-1", 1}, {"invalid", 1},
	} {
		t.Run("nth="+tc.parameter, func(t *testing.T) {
			calls := 0
			// Leave Errors nil: the adapter must not load local context itself.
			h := &Handler{Correlation: correlatorFunc(func(query inport.CorrelateQuery) (*domain.Correlation, error) {
				calls++
				if query.Fingerprint != "abcd" || query.Nth != tc.nth {
					t.Fatalf("query = %+v", query)
				}
				return &domain.Correlation{
					Error:      domain.CorrelateData{Fingerprint: "abcdef0123456789", Type: "CheckoutError", Value: "failed"},
					Occurrence: &domain.CorrelatedOccurrence{Nth: query.Nth, Timestamp: "2026-09-28T12:00:00Z", TraceID: "trace-1"},
					Logs:       []domain.JournalEntry{{Message: "context"}},
				}, nil
			})}
			w := httptest.NewRecorder()
			h.HandleCorrelate(w, httptest.NewRequest(http.MethodGet, "/api/0/correlate/abcd/?nth="+tc.parameter, nil))
			if w.Code != http.StatusOK || calls != 1 {
				t.Fatalf("status = %d, calls = %d", w.Code, calls)
			}
			var result httpwire.Correlation
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Fingerprint != "abcdef0123456789" || result.Type != "CheckoutError" || result.Occurrence == nil || result.Occurrence.Nth != tc.nth || result.Occurrence.TraceID != "trace-1" || len(result.Logs) != 1 {
				t.Fatalf("response = %+v", result)
			}
		})
	}
}

func TestCorrelateHandlerPreservesPartialResultsAndErrors(t *testing.T) {
	for _, failed := range []bool{false, true} {
		h := &Handler{Correlation: correlatorFunc(func(inport.CorrelateQuery) (*domain.Correlation, error) {
			if failed {
				return nil, domain.ErrErrorNotFound
			}
			return &domain.Correlation{Error: domain.CorrelateData{Fingerprint: "abcdef0123456789", Value: "failed"}}, nil
		})}
		w := httptest.NewRecorder()
		h.HandleCorrelate(w, httptest.NewRequest(http.MethodGet, "/api/0/correlate/abcd/", nil))
		if failed {
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", w.Code)
			}
			continue
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || result["value"] == nil || result["occurrence"] != nil || result["logs"] != nil {
			t.Fatalf("partial response = %s", w.Body.String())
		}
	}
}
