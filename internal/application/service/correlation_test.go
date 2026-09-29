package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	inport "github.com/PhilHem/drillip/internal/application/port/in"
	outport "github.com/PhilHem/drillip/internal/application/port/out"
	"github.com/PhilHem/drillip/internal/domain"
)

const correlationFingerprint = "abcdef0123456789"

// Unused methods panic through the embedded nil port.
type correlationRepository struct {
	outport.Repository
	detail                              domain.CorrelateData
	occurrence                          domain.Occurrence
	lookupErr, detailErr, occurrenceErr error
	prefix, detailFP, occurrenceFP      string
	nth                                 int
}

func (r *correlationRepository) FindByPrefix(prefix string) (string, error) {
	r.prefix = prefix
	return correlationFingerprint, r.lookupErr
}
func (r *correlationRepository) GetCorrelateData(fp string) (*domain.CorrelateData, error) {
	r.detailFP = fp
	return &r.detail, r.detailErr
}
func (r *correlationRepository) GetNthOccurrence(fp string, nth int) (*domain.Occurrence, error) {
	r.occurrenceFP, r.nth = fp, nth
	return &r.occurrence, r.occurrenceErr
}

func correlationRepo() *correlationRepository {
	return &correlationRepository{
		detail:     domain.CorrelateData{Fingerprint: correlationFingerprint, Type: "CheckoutError", Value: "checkout failed", Stacktrace: `{"frames":[]}`, Breadcrumbs: `[]`, UserContext: `{"id":"42"}`},
		occurrence: domain.Occurrence{Timestamp: "2026-09-28T14:00:00+02:00", TraceID: "trace-1"},
	}
}

type telemetryRecorder struct {
	calls   []string
	times   []time.Time
	traceID string
	failing string
}

func (r *telemetryRecorder) record(source string) error {
	r.calls = append(r.calls, source)
	if r.failing == source {
		return errors.New("source unavailable")
	}
	return nil
}
func (r *telemetryRecorder) Logs(at time.Time) ([]domain.JournalEntry, error) {
	r.times = append(r.times, at)
	return []domain.JournalEntry{{Message: "log"}}, r.record("logs")
}
func (r *telemetryRecorder) Trace(id string) (*domain.TraceData, error) {
	r.traceID = id
	return &domain.TraceData{ServiceName: "api"}, r.record("trace")
}
func (r *telemetryRecorder) Metrics(at time.Time) (*domain.MetricsSnapshot, error) {
	r.times = append(r.times, at)
	return &domain.MetricsSnapshot{Values: map[string]string{"cpu": "0.5"}}, r.record("metrics")
}
func (r *telemetryRecorder) Profile(at time.Time) ([]domain.ProfileEntry, error) {
	r.times = append(r.times, at)
	return []domain.ProfileEntry{{Function: "main"}}, r.record("profile")
}

func TestCorrelateResolvesPrefixAndSelectsOccurrence(t *testing.T) {
	repo := correlationRepo()
	telemetry := &telemetryRecorder{}
	result, err := New(repo, nil, telemetry).Correlate(context.Background(), inport.CorrelateQuery{Fingerprint: "abcd", Nth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if repo.prefix != "abcd" || repo.detailFP != correlationFingerprint || repo.occurrenceFP != correlationFingerprint || repo.nth != 2 {
		t.Fatalf("repository query = %+v", repo)
	}
	if result.Error != repo.detail {
		t.Fatalf("error data = %+v", result.Error)
	}
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	occ := result.Occurrence
	if occ == nil || occ.Nth != 2 || occ.Timestamp != repo.occurrence.Timestamp || !occ.Time.Equal(at) || occ.TraceID != "trace-1" {
		t.Fatalf("occurrence = %+v", occ)
	}
	if telemetry.traceID != "trace-1" || len(telemetry.times) != 3 {
		t.Fatalf("telemetry inputs = %+v", telemetry)
	}
	for _, got := range telemetry.times {
		if !got.Equal(at) {
			t.Fatalf("telemetry timestamp = %v, want %v", got, at)
		}
	}
}

func TestCorrelateReturnsErrorLookupFailures(t *testing.T) {
	lookupErr := errors.New("error unavailable")
	for _, stage := range []string{"prefix", "detail"} {
		t.Run(stage, func(t *testing.T) {
			repo := correlationRepo()
			if stage == "prefix" {
				repo.lookupErr = lookupErr
			} else {
				repo.detailErr = lookupErr
			}
			telemetry := &telemetryRecorder{}
			result, err := New(repo, nil, telemetry).Correlate(context.Background(), inport.CorrelateQuery{Fingerprint: "abcd", Nth: 1})
			if !errors.Is(err, lookupErr) || result != nil {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
			if repo.occurrenceFP != "" || len(telemetry.calls) != 0 {
				t.Fatal("queried optional context after error lookup failed")
			}
			if stage == "prefix" && repo.detailFP != "" {
				t.Fatal("loaded details without resolving the error")
			}
		})
	}
}

func TestCorrelateKeepsErrorWhenOccurrenceUnavailable(t *testing.T) {
	repo := correlationRepo()
	repo.occurrenceErr = errors.New("occurrence unavailable")
	telemetry := &telemetryRecorder{}
	result, err := New(repo, nil, telemetry).Correlate(context.Background(), inport.CorrelateQuery{Fingerprint: "abcd", Nth: 99})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != repo.detail || result.Occurrence != nil {
		t.Fatalf("result = %+v", result)
	}
	if len(telemetry.calls) != 0 {
		t.Fatalf("unexpected telemetry: %v", telemetry.calls)
	}
}

func TestCorrelateKeepsOtherSourcesWhenOneFails(t *testing.T) {
	for _, failing := range []string{"logs", "trace", "metrics", "profile"} {
		t.Run(failing, func(t *testing.T) {
			telemetry := &telemetryRecorder{failing: failing}
			result, err := New(correlationRepo(), nil, telemetry).Correlate(context.Background(), inport.CorrelateQuery{Fingerprint: "abcd", Nth: 1})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(telemetry.calls, []string{"logs", "trace", "metrics", "profile"}) {
				t.Fatalf("calls = %v", telemetry.calls)
			}
			if (len(result.Logs) == 0) != (failing == "logs") || (result.Trace == nil) != (failing == "trace") || (result.Metrics == nil) != (failing == "metrics") || (len(result.Profile) == 0) != (failing == "profile") {
				t.Fatalf("failed source %s, result = %+v", failing, result)
			}
			if result.Occurrence == nil || result.Error.Value != "checkout failed" {
				t.Fatal("lost base context")
			}
		})
	}
}

func TestCorrelateSkipsSourcesWithoutRequiredContext(t *testing.T) {
	for _, tc := range []struct {
		name, timestamp, traceID string
		calls                    []string
	}{
		{"invalid timestamp", "invalid", "trace-1", []string{"trace"}},
		{"missing trace", "2026-09-28T12:00:00Z", "", []string{"logs", "metrics", "profile"}},
		{"no usable context", "", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := correlationRepo()
			repo.occurrence = domain.Occurrence{Timestamp: tc.timestamp, TraceID: tc.traceID}
			telemetry := &telemetryRecorder{}
			result, err := New(repo, nil, telemetry).Correlate(context.Background(), inport.CorrelateQuery{Fingerprint: "abcd", Nth: 1})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(telemetry.calls, tc.calls) {
				t.Fatalf("calls = %v, want %v", telemetry.calls, tc.calls)
			}
			if result.Occurrence == nil || result.Occurrence.Timestamp != tc.timestamp {
				t.Fatal("lost raw occurrence data")
			}
			if tc.timestamp != "2026-09-28T12:00:00Z" && !result.Occurrence.Time.IsZero() {
				t.Fatal("invalid timestamp must yield zero time")
			}
		})
	}
}

func TestCorrelateWithoutTelemetryKeepsLocalContext(t *testing.T) {
	repo := correlationRepo()
	result, err := New(repo, nil, nil).Correlate(context.Background(), inport.CorrelateQuery{Fingerprint: "abcd", Nth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != repo.detail || result.Occurrence == nil {
		t.Fatalf("result = %+v", result)
	}
	if result.Logs != nil || result.Trace != nil || result.Metrics != nil || result.Profile != nil {
		t.Fatal("unexpected telemetry")
	}
}
