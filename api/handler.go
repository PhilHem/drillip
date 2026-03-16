package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Handler serves the JSON API endpoints.
type Handler struct {
	DB *sql.DB
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
	TagDist     map[string]tagDist `json:"tag_distribution,omitempty"`
}

type tagDist struct {
	Values []tagValue `json:"values"`
}

type tagValue struct {
	Value   string `json:"value"`
	Count   int    `json:"count"`
	Percent int    `json:"percent"`
}

type apiStats struct {
	UniqueErrors     int    `json:"unique_errors"`
	TotalOccurrences int    `json:"total_occurrences"`
	FirstSeen        string `json:"first_seen,omitempty"`
	LastSeen         string `json:"last_seen,omitempty"`
}

// deriveState returns "resolved", "new", or "ongoing" based on resolved_at and first_seen.
func deriveState(resolvedAt, firstSeen string) string {
	if resolvedAt != "" {
		return "resolved"
	}
	if t, err := time.Parse(time.RFC3339, firstSeen); err == nil {
		if time.Since(t) < time.Hour {
			return "new"
		}
	}
	return "ongoing"
}

// parseTag splits "key=value" into (key, value, true) or ("", "", false).
func parseTag(s string) (string, string, bool) {
	i := strings.IndexByte(s, '=')
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	suffix := s[len(s)-1]
	numStr := s[:len(s)-1]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	switch suffix {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown suffix %q (use h/d/w)", string(suffix))
	}
}

func (h *Handler) HandleTop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		if k, v, ok := parseTag(tag); ok {
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
		http.Error(w, "internal error", http.StatusInternalServerError)
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
		e.State = deriveState(resolvedAt, firstSeen)
		results = append(results, e)
	}

	writeJSON(w, results)
}

func (h *Handler) HandleShow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract fingerprint from path: /api/0/show/<fp>/
	path := strings.TrimPrefix(r.URL.Path, "/api/0/show/")
	fp := strings.TrimSuffix(path, "/")
	if fp == "" {
		http.Error(w, "missing fingerprint", http.StatusBadRequest)
		return
	}

	var d apiErrorDetail
	var stacktrace, breadcrumbs, userCtx, tags, resolvedAt string
	err := h.DB.QueryRow(`
		SELECT fingerprint, type, value, level, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform,
			first_seen, last_seen, count, COALESCE(resolved_at, '')
		FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1
	`, fp).Scan(&d.Fingerprint, &d.Type, &d.Value, &d.Level, &stacktrace, &breadcrumbs,
		&d.Release, &d.Environment, &userCtx, &tags, &d.Platform,
		&d.FirstSeen, &d.LastSeen, &d.Count, &resolvedAt)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	d.ResolvedAt = resolvedAt
	d.State = deriveState(resolvedAt, d.FirstSeen)

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

	d.TagDist = h.queryTagDistribution(d.Fingerprint)

	writeJSON(w, d)
}

func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var s apiStats
	_ = h.DB.QueryRow("SELECT COUNT(*) FROM errors").Scan(&s.UniqueErrors)
	_ = h.DB.QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&s.TotalOccurrences)
	_ = h.DB.QueryRow("SELECT MIN(first_seen) FROM errors").Scan(&s.FirstSeen)
	_ = h.DB.QueryRow("SELECT MAX(last_seen) FROM errors").Scan(&s.LastSeen)

	writeJSON(w, s)
}

func (h *Handler) queryTagDistribution(fp string) map[string]tagDist {
	rows, err := h.DB.Query(`SELECT tags FROM occurrences WHERE fingerprint = ? AND tags != '' AND tags IS NOT NULL`, fp)
	if err != nil {
		return nil
	}
	defer rows.Close()

	dist := map[string]map[string]int{}
	total := 0

	for rows.Next() {
		var tagsJSON string
		if err := rows.Scan(&tagsJSON); err != nil || tagsJSON == "" {
			continue
		}
		var tagMap map[string]string
		if json.Unmarshal([]byte(tagsJSON), &tagMap) != nil {
			continue
		}
		total++
		for k, v := range tagMap {
			if dist[k] == nil {
				dist[k] = map[string]int{}
			}
			dist[k][v]++
		}
	}

	if len(dist) == 0 {
		return nil
	}

	result := map[string]tagDist{}
	for k, values := range dist {
		var td tagDist
		for v, c := range values {
			pct := 0
			if total > 0 {
				pct = c * 100 / total
			}
			td.Values = append(td.Values, tagValue{Value: v, Count: c, Percent: pct})
		}
		result[k] = td
	}
	return result
}

func (h *Handler) HandleRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hours := 1
	if hStr := r.URL.Query().Get("hours"); hStr != "" {
		if n, err := strconv.Atoi(hStr); err == nil && n > 0 {
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
		if k, v, ok := parseTag(tag); ok {
			query += ` AND json_extract(tags, '$.'||?) = ?`
			queryArgs = append(queryArgs, k, v)
		}
	}
	query += ` ORDER BY first_seen DESC`

	rows, err := h.DB.Query(query, queryArgs...)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
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
		e.State = deriveState(resolvedAt, e.LastSeen)
		results = append(results, e)
	}

	writeJSON(w, results)
}

type apiBucket struct {
	Hour  string `json:"hour"`
	Count int    `json:"count"`
}

func (h *Handler) HandleTrend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fp := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/0/trend/"), "/")
	if fp == "" {
		http.Error(w, "missing fingerprint", http.StatusBadRequest)
		return
	}

	var fullFP string
	if err := h.DB.QueryRow("SELECT fingerprint FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1", fp).Scan(&fullFP); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
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
		http.Error(w, "internal error", http.StatusInternalServerError)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fp := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/0/releases/"), "/")
	if fp == "" {
		http.Error(w, "missing fingerprint", http.StatusBadRequest)
		return
	}

	var fullFP string
	if err := h.DB.QueryRow("SELECT fingerprint FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1", fp).Scan(&fullFP); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	rows, err := h.DB.Query(`
		SELECT COALESCE(release_tag, ''), COUNT(*),
			MIN(timestamp), MAX(timestamp)
		FROM occurrences WHERE fingerprint = ?
		GROUP BY release_tag ORDER BY COUNT(*) DESC
	`, fullFP)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	durStr := r.URL.Query().Get("older_than")
	if durStr == "" {
		http.Error(w, "missing older_than param (e.g., 7d, 30d, 24h)", http.StatusBadRequest)
		return
	}

	dur, err := parseDuration(durStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	threshold := time.Now().UTC().Add(-dur).Format(time.RFC3339)
	res, err := h.DB.Exec("DELETE FROM occurrences WHERE timestamp < ?", threshold)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	deleted, _ := res.RowsAffected()

	writeJSON(w, apiGCResult{Deleted: deleted, Threshold: threshold})
}

func (h *Handler) HandleResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fp := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/0/resolve/"), "/")
	if fp == "" {
		http.Error(w, "missing fingerprint", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := h.DB.Exec(
		`UPDATE errors SET resolved_at = ? WHERE fingerprint LIKE ?||'%' AND resolved_at IS NULL`,
		now, fp,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		http.Error(w, "not found or already resolved", http.StatusNotFound)
		return
	}

	// Fetch the full fingerprint for the response
	var fullFP string
	_ = h.DB.QueryRow("SELECT fingerprint FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1", fp).Scan(&fullFP)

	writeJSON(w, map[string]interface{}{
		"fingerprint": fullFP,
		"resolved_at": now,
	})
}

// writeJSON is a helper to write a value as JSON with the appropriate headers.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(v)
}
