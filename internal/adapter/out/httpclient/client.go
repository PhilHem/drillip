// Package httpclient connects command operations to a Drillip server.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PhilHem/drillip/internal/adapter/httpwire"
	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/domain"
)

// Client owns HTTP transport, deadlines, wire decoding, and operation errors.
// It is reusable; each operation receives its caller's context. Mutations are
// never retried or redirected, and failure never selects a local database.
type Client struct {
	base *url.URL
	http *http.Client
}

var _ inport.Errors = (*Client)(nil)
var _ inport.Correlator = (*Client)(nil)

func New(server string) (*Client, error) {
	base, err := url.Parse(server)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("server must be an http or https URL without credentials, query, or fragment")
	}
	return &Client{base: base, http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) request(ctx context.Context, method, path string, query url.Values, result any, required ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if path != "/-/healthy" {
		version, err := c.capabilities(ctx)
		if err != nil {
			return err
		}
		if path == "/api/0/list/" && !slices.Contains(version.Features, httpwire.FeatureErrorList) {
			return fmt.Errorf("server does not support drillip list; upgrade the server together with the CLI")
		}
	}
	return c.exchange(ctx, method, path, query, result, required...)
}

func (c *Client) capabilities(ctx context.Context) (httpwire.Capabilities, error) {
	var version httpwire.Capabilities
	if err := c.exchange(ctx, http.MethodGet, "/api/0/capabilities/", nil, &version, "command_api"); err != nil {
		return version, fmt.Errorf("server command API unavailable; upgrade the server together with the CLI: %w", err)
	}
	if version.CommandAPI != 1 {
		return version, fmt.Errorf("unsupported server command API %d; upgrade the server together with the CLI", version.CommandAPI)
	}
	return version, nil
}

func (c *Client) exchange(ctx context.Context, method, path string, query url.Values, result any, required ...string) error {

	target := *c.base
	target.Path = strings.TrimRight(target.Path, "/") + path
	target.RawPath = ""
	target.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer resp.Body.Close()
	const maxResponse = 16 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(data) > maxResponse {
		return fmt.Errorf("response exceeds %d bytes", maxResponse)
	}
	if resp.StatusCode != http.StatusOK {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &body)
		if body.Error == "" {
			body.Error = http.StatusText(resp.StatusCode)
		}
		var cause error
		switch resp.StatusCode {
		case 404:
			cause = domain.ErrErrorNotFound
		case 409:
			cause = domain.ErrAmbiguousFingerprint
		case 400:
			if body.Error == "invalid fingerprint" {
				cause = domain.ErrInvalidFingerprint
			}
		}
		if cause != nil {
			return fmt.Errorf("HTTP %d: %s: %w", resp.StatusCode, body.Error, cause)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body.Error)
	}
	if result == nil {
		return nil
	}
	if len(required) > 0 {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return fmt.Errorf("invalid response: %w", err)
		}
		for _, key := range required {
			if len(fields[key]) == 0 || bytes.Equal(fields[key], []byte("null")) {
				return fmt.Errorf("invalid response: missing %s", key)
			}
		}
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("invalid response: %w", err)
	}
	return nil
}

func referencePath(operation, ref string) (string, error) {
	if !domain.ValidFingerprint(ref) {
		return "", domain.ErrInvalidFingerprint
	}
	return "/api/0/" + operation + "/" + ref + "/", nil
}
func canonical(fp string) error {
	if len(fp) != 16 || !domain.ValidFingerprint(fp) {
		return fmt.Errorf("invalid response fingerprint")
	}
	return nil
}
func filterQuery(f domain.ListFilter) url.Values {
	q := url.Values{}
	if f.Level != "" {
		q.Set("level", f.Level)
	}
	if f.TagKey != "" {
		q.Set("tag", f.TagKey+"="+f.TagVal)
	}
	return q
}
func timestamp(t time.Time) string { return t.Format(time.RFC3339Nano) }
func (c *Client) list(ctx context.Context, path string, q url.Values) ([]domain.ErrorSummary, error) {
	var data []httpwire.Error
	if err := c.request(ctx, http.MethodGet, path, q, &data); err != nil {
		return nil, err
	}
	result := make([]domain.ErrorSummary, len(data))
	for i, d := range data {
		if err := canonical(d.Fingerprint); err != nil {
			return nil, err
		}
		result[i] = domain.ErrorSummary{Fingerprint: d.Fingerprint, Count: d.Count, Level: d.Level, Type: d.Type, Value: d.Value, FirstSeen: d.FirstSeen, LastSeen: d.LastSeen, ResolvedAt: d.ResolvedAt, State: d.State}
	}
	return result, nil
}
func (c *Client) ListTop(ctx context.Context, f domain.ListFilter, limit int) ([]domain.ErrorSummary, error) {
	q := filterQuery(f)
	q.Set("limit", strconv.Itoa(limit))
	return c.list(ctx, "/api/0/top/", q)
}
func (c *Client) ListRecent(ctx context.Context, f domain.ListFilter, since time.Time) ([]domain.ErrorSummary, error) {
	q := filterQuery(f)
	q.Set("since", timestamp(since))
	return c.list(ctx, "/api/0/recent/", q)
}
func (c *Client) GetDetail(ctx context.Context, ref string) (*domain.ErrorDetail, error) {
	path, err := referencePath("show", ref)
	if err != nil {
		return nil, err
	}
	var d httpwire.ErrorDetail
	if err := c.request(ctx, http.MethodGet, path, nil, &d, "fingerprint", "count", "first_seen", "last_seen"); err != nil {
		return nil, err
	}
	if err := canonical(d.Fingerprint); err != nil {
		return nil, err
	}
	return &domain.ErrorDetail{Fingerprint: d.Fingerprint, Count: d.Count, Level: d.Level, Type: d.Type, Value: d.Value, Release: d.Release, Environment: d.Environment, Platform: d.Platform, FirstSeen: d.FirstSeen, LastSeen: d.LastSeen, ResolvedAt: d.ResolvedAt, State: d.State, Stacktrace: string(d.Stacktrace), Breadcrumbs: string(d.Breadcrumbs), UserContext: string(d.User), Tags: string(d.Tags), TagDist: d.TagDist}, nil
}
func (c *Client) GetTrend(ctx context.Context, ref string, since time.Time) (domain.Trend, error) {
	path, err := referencePath("trend", ref)
	if err != nil {
		return domain.Trend{}, err
	}
	var d struct {
		Fingerprint string            `json:"fingerprint"`
		Buckets     []httpwire.Bucket `json:"buckets"`
	}
	if err := c.request(ctx, http.MethodGet, path, url.Values{"since": {timestamp(since)}}, &d, "fingerprint", "buckets"); err != nil {
		return domain.Trend{}, err
	}
	if err := canonical(d.Fingerprint); err != nil {
		return domain.Trend{}, err
	}
	result := domain.Trend{Fingerprint: d.Fingerprint}
	for _, b := range d.Buckets {
		if _, err := time.Parse("2006-01-02 15:00", b.Hour); err != nil {
			return domain.Trend{}, fmt.Errorf("invalid trend timestamp: %w", err)
		}
		result.Buckets = append(result.Buckets, domain.TrendBucket{Hour: b.Hour, Count: b.Count})
	}
	return result, nil
}
func (c *Client) GetReleases(ctx context.Context, ref string) (domain.Releases, error) {
	path, err := referencePath("releases", ref)
	if err != nil {
		return domain.Releases{}, err
	}
	var d struct {
		Fingerprint string             `json:"fingerprint"`
		Releases    []httpwire.Release `json:"releases"`
	}
	if err := c.request(ctx, http.MethodGet, path, nil, &d, "fingerprint", "releases"); err != nil {
		return domain.Releases{}, err
	}
	if err := canonical(d.Fingerprint); err != nil {
		return domain.Releases{}, err
	}
	result := domain.Releases{Fingerprint: d.Fingerprint}
	for _, r := range d.Releases {
		result.Releases = append(result.Releases, domain.ReleaseStats{Release: r.Release, Count: r.Count, FirstSeen: r.FirstSeen, LastSeen: r.LastSeen})
	}
	return result, nil
}
func (c *Client) GetStats(ctx context.Context) (domain.OverviewStats, error) {
	var d httpwire.Stats
	if err := c.request(ctx, http.MethodGet, "/api/0/stats/", nil, &d, "unique_errors", "total_occurrences"); err != nil {
		return domain.OverviewStats{}, err
	}
	return domain.OverviewStats{UniqueErrors: d.UniqueErrors, TotalOccurrences: d.TotalOccurrences, FirstSeen: d.FirstSeen, LastSeen: d.LastSeen}, nil
}
func (c *Client) GCOccurrences(ctx context.Context, before time.Time) (int64, error) {
	var d httpwire.GCResult
	err := c.request(ctx, http.MethodPost, "/api/0/gc/", url.Values{"before": {timestamp(before)}}, &d, "deleted", "threshold")
	return d.Deleted, err
}
func (c *Client) Resolve(ctx context.Context, ref string) (domain.ResolveResult, error) {
	path, err := referencePath("resolve", ref)
	if err != nil {
		return domain.ResolveResult{}, err
	}
	var d struct {
		Fingerprint string `json:"fingerprint"`
		ResolvedAt  string `json:"resolved_at"`
	}
	if err := c.request(ctx, http.MethodPost, path, nil, &d, "fingerprint", "resolved_at"); err != nil {
		return domain.ResolveResult{}, err
	}
	if err := canonical(d.Fingerprint); err != nil {
		return domain.ResolveResult{}, err
	}
	if _, err := time.Parse(time.RFC3339Nano, d.ResolvedAt); err != nil {
		return domain.ResolveResult{}, fmt.Errorf("invalid resolution timestamp: %w", err)
	}
	return domain.ResolveResult{Matched: 1, Fingerprint: d.Fingerprint, ResolvedAt: d.ResolvedAt}, nil
}
func (c *Client) Silence(ctx context.Context, ref string, expiresAt *time.Time, reason string) (domain.SilenceResult, error) {
	path, err := referencePath("silence", ref)
	if err != nil {
		return domain.SilenceResult{}, err
	}
	q := url.Values{"reason": {reason}}
	if expiresAt != nil {
		q.Set("expires_at", timestamp(*expiresAt))
	}
	var d struct {
		Fingerprint string `json:"fingerprint"`
		Status      string `json:"status"`
		ExpiresAt   string `json:"expires_at"`
	}
	if err := c.request(ctx, http.MethodPost, path, q, &d, "fingerprint", "status"); err != nil {
		return domain.SilenceResult{}, err
	}
	if err := canonical(d.Fingerprint); err != nil {
		return domain.SilenceResult{}, err
	}
	if d.Status != "silenced" {
		return domain.SilenceResult{}, fmt.Errorf("invalid silence status")
	}
	result := domain.SilenceResult{Fingerprint: d.Fingerprint}
	if d.ExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339Nano, d.ExpiresAt)
		if err != nil {
			return domain.SilenceResult{}, fmt.Errorf("invalid silence expiry: %w", err)
		}
		result.ExpiresAt = &expiry
	}
	if (expiresAt == nil) != (result.ExpiresAt == nil) {
		return domain.SilenceResult{}, fmt.Errorf("server returned a different silence lifetime")
	}
	return result, nil
}
func (c *Client) Unsilence(ctx context.Context, ref string) (string, error) {
	path, err := referencePath("silence", ref)
	if err != nil {
		return "", err
	}
	var d struct {
		Fingerprint string `json:"fingerprint"`
		Status      string `json:"status"`
	}
	if err := c.request(ctx, http.MethodDelete, path, nil, &d, "fingerprint", "status"); err != nil {
		return "", err
	}
	if !domain.ValidFingerprint(d.Fingerprint) || d.Status != "unsilenced" {
		return "", fmt.Errorf("invalid unsilence response")
	}
	return d.Fingerprint, nil
}
func (c *Client) ListSilences(ctx context.Context) ([]domain.SilenceEntry, error) {
	var data []httpwire.Silence
	if err := c.request(ctx, http.MethodGet, "/api/0/silences/", nil, &data); err != nil {
		return nil, err
	}
	result := make([]domain.SilenceEntry, len(data))
	for i, d := range data {
		result[i] = domain.SilenceEntry{Fingerprint: d.Fingerprint, CreatedAt: d.CreatedAt, ExpiresAt: d.ExpiresAt, Reason: d.Reason}
	}
	return result, nil
}
func (c *Client) Correlate(ctx context.Context, q inport.CorrelateQuery) (*domain.Correlation, error) {
	path, err := referencePath("correlate", q.Fingerprint)
	if err != nil {
		return nil, err
	}
	var d httpwire.Correlation
	if err := c.request(ctx, http.MethodGet, path, url.Values{"nth": {strconv.Itoa(q.Nth)}}, &d, "fingerprint", "type", "value"); err != nil {
		return nil, err
	}
	if err := canonical(d.Fingerprint); err != nil {
		return nil, err
	}
	result := &domain.Correlation{Error: domain.CorrelateData{Fingerprint: d.Fingerprint, Type: d.Type, Value: d.Value, Stacktrace: string(d.Stacktrace), Breadcrumbs: string(d.Breadcrumbs), UserContext: string(d.User)}}
	if o := d.Occurrence; o != nil {
		at, _ := time.Parse(time.RFC3339Nano, o.Timestamp)
		result.Occurrence = &domain.CorrelatedOccurrence{Nth: o.Nth, Timestamp: o.Timestamp, Time: at, TraceID: o.TraceID}
	}
	for _, l := range d.Logs {
		result.Logs = append(result.Logs, domain.JournalEntry{Timestamp: l.Timestamp, Message: l.Message, Priority: l.Priority})
	}
	if d.Trace != nil {
		result.Trace = &domain.TraceData{ServiceName: d.Trace.ServiceName}
		for _, s := range d.Trace.Spans {
			duration, err := time.ParseDuration(s.Duration)
			if err != nil {
				return nil, fmt.Errorf("invalid span duration: %w", err)
			}
			result.Trace.Spans = append(result.Trace.Spans, domain.TraceSpan{OperationName: s.OperationName, Duration: duration})
		}
	}
	if d.Metrics != nil {
		result.Metrics = &domain.MetricsSnapshot{Values: d.Metrics}
	}
	for _, p := range d.Profile {
		result.Profile = append(result.Profile, domain.ProfileEntry{Function: p.Function})
	}
	return result, nil
}
func (c *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return c.request(ctx, http.MethodGet, "/-/healthy", nil, nil)
}
