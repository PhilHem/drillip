package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type apiError struct {
	Fingerprint string `json:"fingerprint"`
	Count       int    `json:"count"`
	Level       string `json:"level"`
	Type        string `json:"type"`
	Value       string `json:"value"`
	LastSeen    string `json:"last_seen"`
}

type apiErrorDetail struct {
	Fingerprint string            `json:"fingerprint"`
	Count       int               `json:"count"`
	Level       string            `json:"level"`
	Type        string            `json:"type"`
	Value       string            `json:"value"`
	Release     string            `json:"release,omitempty"`
	Environment string            `json:"environment,omitempty"`
	Platform    string            `json:"platform,omitempty"`
	FirstSeen   string            `json:"first_seen"`
	LastSeen    string            `json:"last_seen"`
	Stacktrace  json.RawMessage   `json:"stacktrace,omitempty"`
	Breadcrumbs json.RawMessage   `json:"breadcrumbs,omitempty"`
	User        json.RawMessage   `json:"user,omitempty"`
	Tags        json.RawMessage   `json:"tags,omitempty"`
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

func handleAPITop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := `SELECT fingerprint, count, type, value, level, last_seen FROM errors`
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

	rows, err := db.Query(query, queryArgs...)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var results []apiError
	for rows.Next() {
		var e apiError
		if err := rows.Scan(&e.Fingerprint, &e.Count, &e.Type, &e.Value, &e.Level, &e.LastSeen); err != nil {
			continue
		}
		results = append(results, e)
	}

	writeJSON(w, results)
}

func handleAPIShow(w http.ResponseWriter, r *http.Request) {
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
	var stacktrace, breadcrumbs, userCtx, tags string
	err := db.QueryRow(`
		SELECT fingerprint, type, value, level, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform,
			first_seen, last_seen, count
		FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1
	`, fp).Scan(&d.Fingerprint, &d.Type, &d.Value, &d.Level, &stacktrace, &breadcrumbs,
		&d.Release, &d.Environment, &userCtx, &tags, &d.Platform,
		&d.FirstSeen, &d.LastSeen, &d.Count)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

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

	d.TagDist = queryTagDistribution(d.Fingerprint)

	writeJSON(w, d)
}

func handleAPIStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var s apiStats
	_ = db.QueryRow("SELECT COUNT(*) FROM errors").Scan(&s.UniqueErrors)
	_ = db.QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&s.TotalOccurrences)
	_ = db.QueryRow("SELECT MIN(first_seen) FROM errors").Scan(&s.FirstSeen)
	_ = db.QueryRow("SELECT MAX(last_seen) FROM errors").Scan(&s.LastSeen)

	writeJSON(w, s)
}

func queryTagDistribution(fp string) map[string]tagDist {
	rows, err := db.Query(`SELECT tags FROM occurrences WHERE fingerprint = ? AND tags != '' AND tags IS NOT NULL`, fp)
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

func handleAPIRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hours := 1
	if h := r.URL.Query().Get("hours"); h != "" {
		if n, err := strconv.Atoi(h); err == nil && n > 0 {
			hours = n
		}
	}

	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339)

	query := `SELECT fingerprint, count, type, value, level, first_seen FROM errors WHERE first_seen > ?`
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

	rows, err := db.Query(query, queryArgs...)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var results []apiError
	for rows.Next() {
		var e apiError
		if err := rows.Scan(&e.Fingerprint, &e.Count, &e.Type, &e.Value, &e.Level, &e.LastSeen); err != nil {
			continue
		}
		results = append(results, e)
	}

	writeJSON(w, results)
}

type apiBucket struct {
	Hour  string `json:"hour"`
	Count int    `json:"count"`
}

func handleAPITrend(w http.ResponseWriter, r *http.Request) {
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
	if err := db.QueryRow("SELECT fingerprint FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1", fp).Scan(&fullFP); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	rows, err := db.Query(`
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

func handleAPIReleases(w http.ResponseWriter, r *http.Request) {
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
	if err := db.QueryRow("SELECT fingerprint FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1", fp).Scan(&fullFP); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	rows, err := db.Query(`
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

func handleAPIGC(w http.ResponseWriter, r *http.Request) {
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
	res, err := db.Exec("DELETE FROM occurrences WHERE timestamp < ?", threshold)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	deleted, _ := res.RowsAffected()

	writeJSON(w, apiGCResult{Deleted: deleted, Threshold: threshold})
}

// writeJSON is a helper to write a value as JSON with the appropriate headers.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(v)
}

