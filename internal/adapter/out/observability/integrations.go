package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// Config holds settings for the correlate command's
// external data sources (logs, traces, metrics, profiles).
type Config struct {
	Unit         string // journalctl unit name
	VMURL        string // VictoriaMetrics base URL
	VTURL        string // VictoriaTraces base URL
	PyroscopeURL string
	Service      string // service name for Pyroscope
}

var httpClient = &http.Client{Timeout: 5 * time.Second}

// execCommand is a variable for test mockability.
var execCommand = exec.CommandContext

// --- Journalctl ---

func QueryJournalctl(ctx context.Context, unit string, ts time.Time) ([]domain.JournalEntry, error) {
	if unit == "" {
		return nil, nil
	}
	since := "@" + strconv.FormatInt(ts.Add(-5*time.Second).Unix(), 10)
	until := "@" + strconv.FormatInt(ts.Add(5*time.Second).Unix(), 10)

	cmd := execCommand(ctx, "journalctl", "-u", unit, "--since", since, "--until", until, "-o", "json", "--no-pager")
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("journalctl: %w", err)
	}

	var entries []domain.JournalEntry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var e domain.JournalEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

// --- VictoriaTraces ---

func QueryVictoriaTraces(ctx context.Context, baseURL, traceID string) (*domain.TraceData, error) {
	if baseURL == "" || traceID == "" {
		return nil, nil
	}

	url := strings.TrimRight(baseURL, "/") + "/api/traces/" + traceID
	resp, err := get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("victoria traces: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("victoria traces: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("victoria traces: read body: %w", err)
	}

	// Parse Jaeger-compatible response
	var result struct {
		Data []struct {
			Processes map[string]struct {
				ServiceName string `json:"serviceName"`
			} `json:"processes"`
			Spans []struct {
				OperationName string `json:"operationName"`
				Duration      int64  `json:"duration"` // microseconds
				Tags          []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				} `json:"tags"`
			} `json:"spans"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("victoria traces: parse: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, nil
	}

	trace := result.Data[0]
	td := &domain.TraceData{}
	for _, p := range trace.Processes {
		td.ServiceName = p.ServiceName
		break
	}
	for _, s := range trace.Spans {
		tags := make(map[string]string)
		for _, tag := range s.Tags {
			tags[tag.Key] = tag.Value
		}
		td.Spans = append(td.Spans, domain.TraceSpan{
			OperationName: s.OperationName,
			Duration:      time.Duration(s.Duration) * time.Microsecond,
			Tags:          tags,
		})
	}
	return td, nil
}

// --- VictoriaMetrics ---

func QueryVictoriaMetrics(ctx context.Context, baseURL string, ts time.Time) (*domain.MetricsSnapshot, error) {
	if baseURL == "" {
		return nil, nil
	}

	queries := map[string]string{
		"error_rate":  `rate(http_requests_total{status=~"5.."}[5m])`,
		"p99_latency": `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))`,
		"cpu_seconds": `process_cpu_seconds_total`,
		"memory_mb":   `process_resident_memory_bytes / 1024 / 1024`,
	}

	snap := &domain.MetricsSnapshot{Values: make(map[string]string)}
	baseAPI := strings.TrimRight(baseURL, "/") + "/api/v1/query"

	for name, query := range queries {
		if ctx.Err() != nil {
			snap.Values[name] = "(timeout)"
			continue
		}
		params := url.Values{
			"query": {query},
			"time":  {strconv.FormatInt(ts.Unix(), 10)},
		}
		resp, err := get(ctx, baseAPI+"?"+params.Encode())
		if err != nil {
			snap.Values[name] = "(error)"
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK {
			snap.Values[name] = "(error)"
			continue
		}

		var result struct {
			Data struct {
				Result []struct {
					Value []json.RawMessage `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &result) != nil {
			snap.Values[name] = "(error)"
			continue
		}
		if len(result.Data.Result) > 1 {
			snap.Values[name] = "(ambiguous: multiple series)"
			continue
		}
		if len(result.Data.Result) == 1 && len(result.Data.Result[0].Value) > 1 {
			var val string
			if json.Unmarshal(result.Data.Result[0].Value[1], &val) == nil {
				snap.Values[name] = val
			}
		}
	}
	return snap, nil
}

// --- Pyroscope ---

func QueryPyroscope(ctx context.Context, baseURL, service string, ts time.Time) ([]domain.ProfileEntry, error) {
	if baseURL == "" || service == "" {
		return nil, nil
	}

	from := ts.Add(-30 * time.Second).Unix()
	until := ts.Add(30 * time.Second).Unix()
	url := fmt.Sprintf("%s/render?query=%s.cpu&from=%d&until=%d&format=json",
		strings.TrimRight(baseURL, "/"), service, from, until)

	resp, err := get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("pyroscope: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pyroscope: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("pyroscope: read body: %w", err)
	}

	var result struct {
		Flamebearer struct {
			Names  []string `json:"names"`
			Levels [][]int  `json:"levels"`
		} `json:"flamebearer"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("pyroscope: parse: %w", err)
	}

	// Extract top functions from names
	var entries []domain.ProfileEntry
	for _, name := range result.Flamebearer.Names {
		if name != "" {
			entries = append(entries, domain.ProfileEntry{Function: name})
		}
	}
	return entries, nil
}

func get(ctx context.Context, target string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}
