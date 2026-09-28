package service

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

type telemetryRecorder struct{ calls []string }

func (r *telemetryRecorder) Logs(time.Time) ([]domain.JournalEntry, error) {
	r.calls = append(r.calls, "logs")
	return nil, errors.New("journal unavailable")
}

func (r *telemetryRecorder) Trace(string) (*domain.TraceData, error) {
	r.calls = append(r.calls, "trace")
	return &domain.TraceData{ServiceName: "api"}, nil
}

func (r *telemetryRecorder) Metrics(time.Time) (*domain.MetricsSnapshot, error) {
	r.calls = append(r.calls, "metrics")
	return &domain.MetricsSnapshot{Values: map[string]string{"cpu": "0.5"}}, nil
}

func (r *telemetryRecorder) Profile(time.Time) ([]domain.ProfileEntry, error) {
	r.calls = append(r.calls, "profile")
	return []domain.ProfileEntry{{Function: "main"}}, nil
}

func TestCorrelationKeepsOtherSourcesWhenOneFails(t *testing.T) {
	telemetry := &telemetryRecorder{}
	result := New(nil, nil, telemetry).Correlate(time.Now(), "trace-1")
	if !reflect.DeepEqual(telemetry.calls, []string{"logs", "trace", "metrics", "profile"}) {
		t.Fatalf("calls = %v", telemetry.calls)
	}
	if len(result.Logs) != 0 || result.Trace.ServiceName != "api" || result.Metrics.Values["cpu"] != "0.5" || len(result.Profile) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCorrelationSkipsSourcesWithoutOccurrenceContext(t *testing.T) {
	telemetry := &telemetryRecorder{}
	app := New(nil, nil, telemetry)
	app.Correlate(time.Time{}, "")
	if len(telemetry.calls) != 0 {
		t.Fatalf("unexpected calls: %v", telemetry.calls)
	}
	app.Correlate(time.Time{}, "trace-1")
	if !reflect.DeepEqual(telemetry.calls, []string{"trace"}) {
		t.Fatalf("calls = %v", telemetry.calls)
	}
	if result := New(nil, nil, nil).Correlate(time.Now(), "trace-1"); result == nil {
		t.Fatal("disabled telemetry must return an empty result")
	}
}
