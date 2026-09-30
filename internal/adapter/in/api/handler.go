package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PhilHem/drillip/internal/adapter/httpwire"

	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/domain"
)

// Handler serves the JSON API endpoints.
type Handler struct {
	Errors        inport.Errors
	Correlation   inport.Correlator
	Notifications inport.Notifications
}

func (h *Handler) HandleTop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var f domain.ListFilter
	f.Level = r.URL.Query().Get("level")
	if tag := r.URL.Query().Get("tag"); tag != "" {
		if k, v, ok := domain.ParseTag(tag); ok {
			f.TagKey, f.TagVal = k, v
		}
	}

	limit := 25
	if value := r.URL.Query().Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			writeError(w, 400, "limit must be a positive integer")
			return
		}
		limit = n
	}
	summaries, err := h.Errors.ListTop(r.Context(), f, limit)
	if err != nil {
		slog.Error("HandleTop", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	results := make([]httpwire.Error, len(summaries))
	for i, s := range summaries {
		results[i] = httpwire.FromSummary(s)
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

	detail, err := h.Errors.GetDetail(r.Context(), fp)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	writeJSON(w, httpwire.FromDetail(detail))
}

func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	stats, err := h.Errors.GetStats(r.Context())
	if err != nil {
		slog.Error("HandleStats", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, httpwire.Stats{
		UniqueErrors:     stats.UniqueErrors,
		TotalOccurrences: stats.TotalOccurrences,
		FirstSeen:        stats.FirstSeen,
		LastSeen:         stats.LastSeen,
	})
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

	since, err := queryTime(r, "since", "hours", time.Now().UTC().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	var f domain.ListFilter
	f.Level = r.URL.Query().Get("level")
	if tag := r.URL.Query().Get("tag"); tag != "" {
		if k, v, ok := domain.ParseTag(tag); ok {
			f.TagKey, f.TagVal = k, v
		}
	}

	summaries, err := h.Errors.ListRecent(r.Context(), f, since)
	if err != nil {
		slog.Error("HandleRecent", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	results := make([]httpwire.Error, len(summaries))
	for i, s := range summaries {
		results[i] = httpwire.FromSummary(s)
	}

	writeJSON(w, results)
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

	since, err := queryTime(r, "since", "", time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	trend, err := h.Errors.GetTrend(r.Context(), fp, since)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	fullFP, trendBuckets := trend.Fingerprint, trend.Buckets
	buckets := make([]httpwire.Bucket, len(trendBuckets))
	for i, b := range trendBuckets {
		buckets[i] = httpwire.Bucket{Hour: b.Hour, Count: b.Count}
	}

	writeJSON(w, map[string]interface{}{
		"fingerprint": fullFP,
		"buckets":     buckets,
	})
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

	result, err := h.Errors.GetReleases(r.Context(), fp)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	fullFP, releaseStats := result.Fingerprint, result.Releases
	releases := make([]httpwire.Release, len(releaseStats))
	for i, r := range releaseStats {
		releases[i] = httpwire.Release{
			Release:   r.Release,
			Count:     r.Count,
			FirstSeen: r.FirstSeen,
			LastSeen:  r.LastSeen,
		}
	}

	writeJSON(w, map[string]interface{}{
		"fingerprint": fullFP,
		"releases":    releases,
	})
}

func (h *Handler) HandleGC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	threshold := time.Time{}
	if durStr := r.URL.Query().Get("older_than"); durStr != "" {
		dur, err := domain.ParseDuration(durStr)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		threshold = time.Now().UTC().Add(-dur)
	}
	threshold, err := queryTime(r, "before", "older_than", threshold)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if threshold.IsZero() {
		writeError(w, 400, "missing before or older_than parameter")
		return
	}
	deleted, err := h.Errors.GCOccurrences(r.Context(), threshold)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, httpwire.GCResult{Deleted: deleted, Threshold: threshold.Format(time.RFC3339)})
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

	result, err := h.Errors.Resolve(r.Context(), fp)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	if result.Matched == 0 {
		writeError(w, http.StatusNotFound, "not found or already resolved")
		return
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
		if r.URL.Query().Has("expires_at") {
			expiry, err := queryTime(r, "expires_at", "duration", time.Time{})
			if err != nil {
				writeError(w, 400, err.Error())
				return
			}
			expiresAt = &expiry
		}
		reason := r.URL.Query().Get("reason")
		if len(reason) > 500 {
			reason = reason[:500]
		}

		result, err := h.Errors.Silence(r.Context(), fp, expiresAt, reason)
		if err != nil {
			writeLookupError(w, err)
			return
		}

		resp := map[string]interface{}{"fingerprint": result.Fingerprint, "status": "silenced"}
		expiresAt = result.ExpiresAt
		if expiresAt != nil {
			resp["expires_at"] = expiresAt.Format(time.RFC3339Nano)
		}
		writeJSON(w, resp)

	case http.MethodDelete:
		fp, err := h.Errors.Unsilence(r.Context(), fp)
		if err != nil {
			writeLookupError(w, err)
			return
		}
		writeJSON(w, map[string]interface{}{"fingerprint": fp, "status": "unsilenced"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) HandleListSilences(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	entries, err := h.Errors.ListSilences(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var results []httpwire.Silence
	for _, e := range entries {
		results = append(results, httpwire.Silence{
			Fingerprint: e.Fingerprint,
			CreatedAt:   e.CreatedAt,
			ExpiresAt:   e.ExpiresAt,
			Reason:      e.Reason,
		})
	}

	writeJSON(w, results)
}

// --- Correlate ---

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

	cr, err := h.Correlation.Correlate(r.Context(), inport.CorrelateQuery{Fingerprint: fp, Nth: nth})
	if err != nil {
		writeLookupError(w, err)
		return
	}
	cd := cr.Error

	result := httpwire.Correlation{
		Fingerprint: cd.Fingerprint,
		Type:        cd.Type,
		Value:       cd.Value,
	}

	if cd.Stacktrace != "" {
		result.Stacktrace = json.RawMessage(cd.Stacktrace)
	}
	if cd.Breadcrumbs != "" {
		result.Breadcrumbs = json.RawMessage(cd.Breadcrumbs)
	}
	if cd.UserContext != "" && cd.UserContext != "null" {
		result.User = json.RawMessage(cd.UserContext)
	}

	if occ := cr.Occurrence; occ != nil {
		result.Occurrence = &httpwire.Occurrence{
			Nth:       occ.Nth,
			Timestamp: occ.Timestamp,
			TraceID:   occ.TraceID,
		}
	}

	for _, e := range cr.Logs {
		result.Logs = append(result.Logs, httpwire.LogEntry{
			Timestamp: e.Timestamp,
			Message:   e.Message,
			Priority:  e.Priority,
		})
	}

	if cr.Trace != nil {
		trace := &httpwire.TraceData{ServiceName: cr.Trace.ServiceName}
		for _, s := range cr.Trace.Spans {
			trace.Spans = append(trace.Spans, httpwire.TraceSpan{
				OperationName: s.OperationName,
				Duration:      s.Duration.String(),
			})
		}
		result.Trace = trace
	}

	if cr.Metrics != nil && len(cr.Metrics.Values) > 0 {
		result.Metrics = cr.Metrics.Values
	}

	for _, e := range cr.Profile {
		result.Profile = append(result.Profile, httpwire.ProfileEntry{Function: e.Function})
	}

	writeJSON(w, result)
}

// HandleTestEmail sends a test email to verify SMTP configuration.
func (h *Handler) HandleTestEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.Notifications == nil {
		writeNotificationsDisabled(w)
		return
	}
	recipient, err := h.Notifications.SendTestEmail()
	if errors.Is(err, inport.ErrNotificationsDisabled) {
		writeNotificationsDisabled(w)
		return
	}
	if err != nil {
		slog.Error("test email failed", "err", err)
		var diagnosis *domain.NotificationError
		if !errors.As(err, &diagnosis) {
			diagnosis = &domain.NotificationError{
				Code:    "smtp_delivery_failed",
				Message: "The SMTP send attempt failed.",
				Hint:    "Check the recipient mailbox and SMTP server logs before retrying; the message may have been accepted.",
			}
		}
		writeNotificationError(w, http.StatusBadGateway, diagnosis)
		return
	}
	writeJSON(w, map[string]string{"status": "sent", "to": recipient})
}

func writeNotificationsDisabled(w http.ResponseWriter) {
	writeNotificationError(w, http.StatusServiceUnavailable, &domain.NotificationError{
		Code:    "notifications_not_configured",
		Message: "notifications not configured",
		Hint:    "Set DRILLIP_SMTP_HOST and DRILLIP_SMTP_TO in the server environment. For Docker, recreate the container; otherwise restart the Drillip server.",
	})
}

func writeNotificationError(w http.ResponseWriter, status int, diagnosis *domain.NotificationError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error": diagnosis.Message,
		"code":  diagnosis.Code,
		"hint":  diagnosis.Hint,
	})
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

// writeLookupError keeps reference failures distinct from storage failures.
func writeLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrAmbiguousFingerprint):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrErrorNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrInvalidFingerprint):
		writeError(w, http.StatusBadRequest, "invalid fingerprint")
	default:
		slog.Error("error lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// queryTime preserves absolute client timestamps and rejects competing clocks.
func queryTime(r *http.Request, absolute, relative string, fallback time.Time) (time.Time, error) {
	q := r.URL.Query()
	if !q.Has(absolute) {
		return fallback, nil
	}
	if relative != "" && q.Has(relative) {
		return time.Time{}, fmt.Errorf("use either %s or %s", absolute, relative)
	}
	value, err := time.Parse(time.RFC3339Nano, q.Get(absolute))
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be an RFC3339 timestamp", absolute)
	}
	return value, nil
}
