package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PhilHem/drillip/domain"
	"github.com/PhilHem/drillip/integrations"
	"github.com/PhilHem/drillip/notify"
	"github.com/PhilHem/drillip/store"
)

// Handler serves the JSON API endpoints.
type Handler struct {
	DB           *sql.DB
	Store        *store.Store
	Integrations integrations.Config
	Notifier     *notify.Notifier
}

type apiError struct {
	Fingerprint string `json:"fingerprint"`
	Count       int    `json:"count"`
	Level       string `json:"level"`
	Type        string `json:"type"`
	Value       string `json:"value"`
	LastSeen    string `json:"last_seen"`
	ResolvedAt  string `json:"resolved_at,omitempty"`
	State       string `json:"state"`
}

type apiErrorDetail struct {
	Fingerprint string             `json:"fingerprint"`
	Count       int                `json:"count"`
	Level       string             `json:"level"`
	Type        string             `json:"type"`
	Value       string             `json:"value"`
	Release     string             `json:"release,omitempty"`
	Environment string             `json:"environment,omitempty"`
	Platform    string             `json:"platform,omitempty"`
	FirstSeen   string             `json:"first_seen"`
	LastSeen    string             `json:"last_seen"`
	ResolvedAt  string             `json:"resolved_at,omitempty"`
	State       string             `json:"state"`
	Stacktrace  json.RawMessage    `json:"stacktrace,omitempty"`
	Breadcrumbs json.RawMessage    `json:"breadcrumbs,omitempty"`
	User        json.RawMessage    `json:"user,omitempty"`
	Tags        json.RawMessage    `json:"tags,omitempty"`
	TagDist     map[string]store.TagDist `json:"tag_distribution,omitempty"`
}

type apiStats struct {
	UniqueErrors     int    `json:"unique_errors"`
	TotalOccurrences int    `json:"total_occurrences"`
	FirstSeen        string `json:"first_seen,omitempty"`
	LastSeen         string `json:"last_seen,omitempty"`
}

func (h *Handler) HandleTop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	query := `SELECT fingerprint, count, type, value, level, last_seen, first_seen, COALESCE(resolved_at, '') FROM errors`
	var conditions []string
	var queryArgs []interface{}

	if level := r.URL.Query().Get("level"); level != "" {
		conditions = append(conditions, `level = ?`)
		queryArgs = append(queryArgs, level)
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		if k, v, ok := domain.ParseTag(tag); ok {
			conditions = append(conditions, `json_extract(tags, '$.'||?) = ?`)
			queryArgs = append(queryArgs, k, v)
		}
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	query += ` ORDER BY count DESC LIMIT 25`

	rows, err := h.DB.Query(query, queryArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	var results []apiError
	for rows.Next() {
		var e apiError
		var firstSeen, resolvedAt string
		if err := rows.Scan(&e.Fingerprint, &e.Count, &e.Type, &e.Value, &e.Level, &e.LastSeen, &firstSeen, &resolvedAt); err != nil {
			continue
		}
		e.ResolvedAt = resolvedAt
		e.State = domain.DeriveState(resolvedAt, firstSeen)
		results = append(results, e)
	}
	if err := rows.Err(); err != nil {
		slog.Error("HandleTop rows iteration", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, results)
}

// extractFingerprint extracts and validates a fingerprint from a URL path.
func extractFingerprint(path, prefix string) (string, bool) {
	fp := strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/")
	if fp == "" || !domain.ValidFingerprint(fp) {
		return "", false
	}
	return fp, true
}

func (h *Handler) HandleShow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	fp, ok := extractFingerprint(r.URL.Path, "/api/0/show/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid fingerprint")
		return
	}

	fullFP, err := h.Store.FindByPrefix(fp)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var d apiErrorDetail
	var stacktrace, breadcrumbs, userCtx, tags, resolvedAt string
	err = h.DB.QueryRow(`
		SELECT type, value, level, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform,
			first_seen, last_seen, count, COALESCE(resolved_at, '')
		FROM errors WHERE fingerprint = ?
	`, fullFP).Scan(&d.Type, &d.Value, &d.Level, &stacktrace, &breadcrumbs,
		&d.Release, &d.Environment, &userCtx, &tags, &d.Platform,
		&d.FirstSeen, &d.LastSeen, &d.Count, &resolvedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	d.Fingerprint = fullFP

	d.ResolvedAt = resolvedAt
	d.State = domain.DeriveState(resolvedAt, d.FirstSeen)

	if stacktrace != "" {
		d.Stacktrace = json.RawMessage(stacktrace)
	}
	if breadcrumbs != "" {
		d.Breadcrumbs = json.RawMessage(breadcrumbs)
	}
	if userCtx != "" && userCtx != "null" {
		d.User = json.RawMessage(userCtx)
	}
	if tags != "" && tags != "null" {
		d.Tags = json.RawMessage(tags)
	}

	d.TagDist = h.Store.GetTagDistribution(d.Fingerprint)

	writeJSON(w, d)
}

func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var s apiStats
	_ = h.DB.QueryRow("SELECT COUNT(*) FROM errors").Scan(&s.UniqueErrors)
	_ = h.DB.QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&s.TotalOccurrences)
	_ = h.DB.QueryRow("SELECT MIN(first_seen) FROM errors").Scan(&s.FirstSeen)
	_ = h.DB.QueryRow("SELECT MAX(last_seen) FROM errors").Scan(&s.LastSeen)

	writeJSON(w, s)
}

func (h *Handler) HandleRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	hours := 1
	if hStr := r.URL.Query().Get("hours"); hStr != "" {
		if n, err := strconv.Atoi(hStr); err == nil && n > 0 {
			if n > 8760 {
				n = 8760
			}
			hours = n
		}
	}

	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339)

	query := `SELECT fingerprint, count, type, value, level, first_seen, COALESCE(resolved_at, '') FROM errors WHERE first_seen > ?`
	queryArgs := []interface{}{since}
	if level := r.URL.Query().Get("level"); level != "" {
		query += ` AND level = ?`
		queryArgs = append(queryArgs, level)
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		if k, v, ok := domain.ParseTag(tag); ok {
			query += ` AND json_extract(tags, '$.'||?) = ?`
			queryArgs = append(queryArgs, k, v)
		}
	}
	query += ` ORDER BY first_seen DESC`

	rows, err := h.DB.Query(query, queryArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	var results []apiError
	for rows.Next() {
		var e apiError
		var resolvedAt string
		if err := rows.Scan(&e.Fingerprint, &e.Count, &e.Type, &e.Value, &e.Level, &e.LastSeen, &resolvedAt); err != nil {
			continue
		}
		e.ResolvedAt = resolvedAt
		e.State = domain.DeriveState(resolvedAt, e.LastSeen)
		results = append(results, e)
	}
	if err := rows.Err(); err != nil {
		slog.Error("HandleRecent rows iteration", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, results)
}

type apiBucket struct {
	Hour  string `json:"hour"`
	Count int    `json:"count"`
}

func (h *Handler) HandleTrend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	fp, ok := extractFingerprint(r.URL.Path, "/api/0/trend/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid fingerprint")
		return
	}

	fullFP, err := h.Store.FindByPrefix(fp)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	rows, err := h.DB.Query(`
		SELECT strftime('%Y-%m-%d %H:00', timestamp) AS hour, COUNT(*) AS cnt
		FROM occurrences
		WHERE fingerprint = ? AND timestamp > ?
		GROUP BY hour ORDER BY hour
	`, fullFP, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	var buckets []apiBucket
	for rows.Next() {
		var b apiBucket
		if err := rows.Scan(&b.Hour, &b.Count); err != nil {
			continue
		}
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		slog.Error("HandleTrend rows iteration", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, map[string]interface{}{
		"fingerprint": fullFP,
		"buckets":     buckets,
	})
}

type apiRelease struct {
	Release   string `json:"release"`
	Count     int    `json:"count"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
}

func (h *Handler) HandleReleases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	fp, ok := extractFingerprint(r.URL.Path, "/api/0/releases/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid fingerprint")
		return
	}

	fullFP, err := h.Store.FindByPrefix(fp)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	rows, err := h.DB.Query(`
		SELECT COALESCE(release_tag, ''), COUNT(*),
			MIN(timestamp), MAX(timestamp)
		FROM occurrences WHERE fingerprint = ?
		GROUP BY release_tag ORDER BY COUNT(*) DESC
	`, fullFP)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	var releases []apiRelease
	for rows.Next() {
		var rel apiRelease
		if err := rows.Scan(&rel.Release, &rel.Count, &rel.FirstSeen, &rel.LastSeen); err != nil {
			continue
		}
		releases = append(releases, rel)
	}
	if err := rows.Err(); err != nil {
		slog.Error("HandleReleases rows iteration", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, map[string]interface{}{
		"fingerprint": fullFP,
		"releases":    releases,
	})
}

type apiGCResult struct {
	Deleted   int64  `json:"deleted"`
	Threshold string `json:"threshold"`
}

func (h *Handler) HandleGC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	durStr := r.URL.Query().Get("older_than")
	if durStr == "" {
		writeError(w, http.StatusBadRequest, "missing older_than param (e.g., 7d, 30d, 24h)")
		return
	}

	dur, err := domain.ParseDuration(durStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	threshold := time.Now().UTC().Add(-dur).Format(time.RFC3339)
	res, err := h.DB.Exec("DELETE FROM occurrences WHERE timestamp < ?", threshold)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	deleted, _ := res.RowsAffected()

	writeJSON(w, apiGCResult{Deleted: deleted, Threshold: threshold})
}

func (h *Handler) HandleResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	fp, ok := extractFingerprint(r.URL.Path, "/api/0/resolve/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid fingerprint")
		return
	}

	result, err := h.Store.Resolve(fp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if result.Matched == 0 {
		writeError(w, http.StatusNotFound, "not found or already resolved")
		return
	}

	if h.Notifier != nil && len(result.Resolved) > 0 {
		go h.Notifier.NotifyResolved(result.Resolved)
	}

	writeJSON(w, map[string]interface{}{
		"fingerprint": result.Fingerprint,
		"resolved_at": result.ResolvedAt,
	})
}

func (h *Handler) HandleSilence(w http.ResponseWriter, r *http.Request) {
	fp, ok := extractFingerprint(r.URL.Path, "/api/0/silence/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid fingerprint")
		return
	}

	switch r.Method {
	case http.MethodPost:
		var expiresAt *time.Time
		if durStr := r.URL.Query().Get("duration"); durStr != "" {
			dur, err := domain.ParseDuration(durStr)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			t := time.Now().UTC().Add(dur)
			expiresAt = &t
		}
		reason := r.URL.Query().Get("reason")
		if len(reason) > 500 {
			reason = reason[:500]
		}

		if err := h.Store.Silence(fp, expiresAt, reason); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		resp := map[string]interface{}{"fingerprint": fp, "status": "silenced"}
		if expiresAt != nil {
			resp["expires_at"] = expiresAt.Format(time.RFC3339)
		}
		writeJSON(w, resp)

	case http.MethodDelete:
		if err := h.Store.Unsilence(fp); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, map[string]interface{}{"fingerprint": fp, "status": "unsilenced"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

type apiSilence struct {
	Fingerprint string `json:"fingerprint"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

func (h *Handler) HandleListSilences(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	entries, err := h.Store.ListSilences()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var results []apiSilence
	for _, e := range entries {
		results = append(results, apiSilence{
			Fingerprint: e.Fingerprint,
			CreatedAt:   e.CreatedAt,
			ExpiresAt:   e.ExpiresAt,
			Reason:      e.Reason,
		})
	}

	writeJSON(w, results)
}

// --- Correlate ---

type apiCorrelation struct {
	Fingerprint string                 `json:"fingerprint"`
	Type        string                 `json:"type"`
	Value       string                 `json:"value"`
	Occurrence  *apiOccurrence         `json:"occurrence,omitempty"`
	Stacktrace  json.RawMessage        `json:"stacktrace,omitempty"`
	Breadcrumbs json.RawMessage        `json:"breadcrumbs,omitempty"`
	User        json.RawMessage        `json:"user,omitempty"`
	Logs        []apiLogEntry          `json:"logs,omitempty"`
	Trace       *apiTraceData          `json:"trace,omitempty"`
	Metrics     map[string]string      `json:"metrics,omitempty"`
	Profile     []apiProfileEntry      `json:"profile,omitempty"`
}

type apiOccurrence struct {
	Nth       int    `json:"nth"`
	Timestamp string `json:"timestamp"`
	TraceID   string `json:"trace_id,omitempty"`
}

type apiLogEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	Priority  string `json:"priority,omitempty"`
}

type apiTraceData struct {
	ServiceName string         `json:"service_name"`
	Spans       []apiTraceSpan `json:"spans"`
}

type apiTraceSpan struct {
	OperationName string `json:"operation_name"`
	Duration      string `json:"duration"`
}

type apiProfileEntry struct {
	Function string `json:"function"`
}

func (h *Handler) HandleCorrelate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	fp, ok := extractFingerprint(r.URL.Path, "/api/0/correlate/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid fingerprint")
		return
	}

	nth := 1
	if n := r.URL.Query().Get("nth"); n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			nth = v
		}
	}

	fullFP, err := h.Store.FindByPrefix(fp)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// Fetch error row
	var typ, val, stacktrace, breadcrumbsJSON, userCtx string
	err = h.DB.QueryRow(`
		SELECT type, value, stacktrace, breadcrumbs, user_context
		FROM errors WHERE fingerprint = ?
	`, fullFP).Scan(&typ, &val, &stacktrace, &breadcrumbsJSON, &userCtx)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	result := apiCorrelation{
		Fingerprint: fullFP,
		Type:        typ,
		Value:       val,
	}

	if stacktrace != "" {
		result.Stacktrace = json.RawMessage(stacktrace)
	}
	if breadcrumbsJSON != "" {
		result.Breadcrumbs = json.RawMessage(breadcrumbsJSON)
	}
	if userCtx != "" && userCtx != "null" {
		result.User = json.RawMessage(userCtx)
	}

	// Fetch Nth most recent occurrence
	var occTimestamp, occTraceID string
	err = h.DB.QueryRow(`
		SELECT timestamp, COALESCE(trace_id, '')
		FROM occurrences WHERE fingerprint = ?
		ORDER BY timestamp DESC LIMIT 1 OFFSET ?
	`, fullFP, nth-1).Scan(&occTimestamp, &occTraceID)

	var occTime time.Time
	if err == nil {
		occTime, _ = time.Parse(time.RFC3339, occTimestamp)
		result.Occurrence = &apiOccurrence{
			Nth:       nth,
			Timestamp: occTimestamp,
			TraceID:   occTraceID,
		}
	}

	cfg := h.Integrations

	// Logs — if unit configured
	if cfg.Unit != "" && !occTime.IsZero() {
		entries, err := integrations.QueryJournalctl(cfg.Unit, occTime)
		if err == nil {
			for _, e := range entries {
				result.Logs = append(result.Logs, apiLogEntry{
					Timestamp: e.Timestamp,
					Message:   e.Message,
					Priority:  e.Priority,
				})
			}
		}
	}

	// Trace — if VT configured and occurrence has trace_id
	if cfg.VTURL != "" && occTraceID != "" {
		td, err := integrations.QueryVictoriaTraces(cfg.VTURL, occTraceID)
		if err == nil && td != nil {
			trace := &apiTraceData{ServiceName: td.ServiceName}
			for _, s := range td.Spans {
				trace.Spans = append(trace.Spans, apiTraceSpan{
					OperationName: s.OperationName,
					Duration:      s.Duration.String(),
				})
			}
			result.Trace = trace
		}
	}

	// Metrics — if VM configured
	if cfg.VMURL != "" && !occTime.IsZero() {
		snap, err := integrations.QueryVictoriaMetrics(cfg.VMURL, occTime)
		if err == nil && snap != nil && len(snap.Values) > 0 {
			result.Metrics = snap.Values
		}
	}

	// Profile — if Pyroscope configured
	if cfg.PyroscopeURL != "" && !occTime.IsZero() {
		entries, err := integrations.QueryPyroscope(cfg.PyroscopeURL, cfg.Service, occTime)
		if err == nil {
			for _, e := range entries {
				result.Profile = append(result.Profile, apiProfileEntry{Function: e.Function})
			}
		}
	}

	writeJSON(w, result)
}

// HandleTestEmail sends a test email to verify SMTP configuration.
func (h *Handler) HandleTestEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.Notifier == nil {
		writeError(w, http.StatusServiceUnavailable, "notifications not configured")
		return
	}
	if err := h.Notifier.SendTestEmail(); err != nil {
		slog.Error("test email failed", "err", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("send failed: %v", err))
		return
	}
	writeJSON(w, map[string]string{"status": "sent", "to": h.Notifier.SMTP.To})
}

// writeError writes a structured JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// writeJSON is a helper to write a value as JSON with the appropriate headers.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
