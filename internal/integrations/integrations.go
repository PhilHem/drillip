package integrations

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
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
var execCommand = exec.Command

// --- Journalctl ---

type JournalEntry struct {
	Timestamp string `json:"__REALTIME_TIMESTAMP"`
	Message   string `json:"MESSAGE"`
	Priority  string `json:"PRIORITY"`
}

func QueryJournalctl(unit string, ts time.Time) ([]JournalEntry, error) {
	if unit == "" {
		return nil, nil
	}
	since := ts.Add(-5 * time.Second).Format("2006-01-02 15:04:05")
	until := ts.Add(5 * time.Second).Format("2006-01-02 15:04:05")

	cmd := execCommand("journalctl", "-u", unit, "--since", since, "--until", until, "-o", "json", "--no-pager")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("journalctl: %w", err)
	}

	var entries []JournalEntry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var e JournalEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

// --- VictoriaTraces ---

type TraceData struct {
	ServiceName string
	Spans       []TraceSpan
}

type TraceSpan struct {
	OperationName string
	Duration      time.Duration
	Tags          map[string]string
}

func QueryVictoriaTraces(baseURL, traceID string) (*TraceData, error) {
	if baseURL == "" || traceID == "" {
		return nil, nil
	}

	url := strings.TrimRight(baseURL, "/") + "/api/traces/" + traceID
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("victoria traces: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("victoria traces: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
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
	td := &TraceData{}
	for _, p := range trace.Processes {
		td.ServiceName = p.ServiceName
		break
	}
	for _, s := range trace.Spans {
		tags := make(map[string]string)
		for _, tag := range s.Tags {
			tags[tag.Key] = tag.Value
		}
		td.Spans = append(td.Spans, TraceSpan{
			OperationName: s.OperationName,
			Duration:      time.Duration(s.Duration) * time.Microsecond,
			Tags:          tags,
		})
	}
	return td, nil
}

// --- VictoriaMetrics ---

type MetricsSnapshot struct {
	Values map[string]string
}

func QueryVictoriaMetrics(baseURL string, ts time.Time) (*MetricsSnapshot, error) {
	if baseURL == "" {
		return nil, nil
	}

	queries := map[string]string{
		"error_rate":   `rate(http_requests_total{status=~"5.."}[5m])`,
		"p99_latency":  `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))`,
		"cpu_usage":    `process_cpu_seconds_total`,
		"memory_mb":    `process_resident_memory_bytes / 1024 / 1024`,
	}

	snap := &MetricsSnapshot{Values: make(map[string]string)}
	baseAPI := strings.TrimRight(baseURL, "/") + "/api/v1/query"

	for name, query := range queries {
		url := fmt.Sprintf("%s?query=%s&time=%d", baseAPI, query, ts.Unix())
		resp, err := httpClient.Get(url)
		if err != nil {
			snap.Values[name] = "(error)"
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result struct {
			Data struct {
				Result []struct {
					Value []json.RawMessage `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &result) == nil && len(result.Data.Result) > 0 && len(result.Data.Result[0].Value) > 1 {
			var val string
			if json.Unmarshal(result.Data.Result[0].Value[1], &val) == nil {
				snap.Values[name] = val
			}
		}
	}
	return snap, nil
}

// --- Pyroscope ---

type ProfileEntry struct {
	Function string
	Self     int64
}

func QueryPyroscope(baseURL, service string, ts time.Time) ([]ProfileEntry, error) {
	if baseURL == "" || service == "" {
		return nil, nil
	}

	from := ts.Add(-30 * time.Second).Unix()
	until := ts.Add(30 * time.Second).Unix()
	url := fmt.Sprintf("%s/render?query=%s.cpu&from=%d&until=%d&format=json",
		strings.TrimRight(baseURL, "/"), service, from, until)

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("pyroscope: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pyroscope: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
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
	var entries []ProfileEntry
	for _, name := range result.Flamebearer.Names {
		if name != "" {
			entries = append(entries, ProfileEntry{Function: name})
		}
	}
	return entries, nil
}
