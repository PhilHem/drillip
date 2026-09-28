package sqlite

import (
	"strings"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// --- Query result types ------------------------------------------------------

// --- Read methods ------------------------------------------------------------

// Ping checks that the database connection is alive.
func (s *Store) Ping() error {
	return s.db.Ping()
}

// ListTop returns errors ordered by occurrence count (descending).
func (s *Store) ListTop(f domain.ListFilter, limit int) ([]domain.ErrorSummary, error) {
	query := `SELECT fingerprint, count, type, value, level, last_seen, first_seen, COALESCE(resolved_at, '') FROM errors`
	var args []any

	var conds []string
	if f.Level != "" {
		conds = append(conds, `level = ?`)
		args = append(args, f.Level)
	}
	if f.TagKey != "" {
		conds = append(conds, `json_extract(tags, '$.'||?) = ?`)
		args = append(args, f.TagKey, f.TagVal)
	}
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	query += ` ORDER BY count DESC LIMIT ?`
	args = append(args, limit)

	return s.queryErrorSummaries(query, args)
}

// ListRecent returns errors first seen after the given time, ordered newest first.
func (s *Store) ListRecent(f domain.ListFilter, since time.Time) ([]domain.ErrorSummary, error) {
	query := `SELECT fingerprint, count, type, value, level, last_seen, first_seen, COALESCE(resolved_at, '') FROM errors WHERE first_seen > ?`
	args := []any{since.UTC().Format(time.RFC3339)}

	if f.Level != "" {
		query += ` AND level = ?`
		args = append(args, f.Level)
	}
	if f.TagKey != "" {
		query += ` AND json_extract(tags, '$.'||?) = ?`
		args = append(args, f.TagKey, f.TagVal)
	}
	query += ` ORDER BY first_seen DESC`

	return s.queryErrorSummaries(query, args)
}

func (s *Store) queryErrorSummaries(query string, args []any) ([]domain.ErrorSummary, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []domain.ErrorSummary
	for rows.Next() {
		var e domain.ErrorSummary
		if err := rows.Scan(&e.Fingerprint, &e.Count, &e.Type, &e.Value, &e.Level, &e.LastSeen, &e.FirstSeen, &e.ResolvedAt); err != nil {
			return nil, err
		}
		e.State = domain.DeriveState(e.ResolvedAt, e.FirstSeen)
		results = append(results, e)
	}
	return results, rows.Err()
}

// GetDetail returns the full detail for a single error by its full fingerprint.
func (s *Store) GetDetail(fp string) (*domain.ErrorDetail, error) {
	var d domain.ErrorDetail
	err := s.db.QueryRow(`
		SELECT type, value, level, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform,
			first_seen, last_seen, count, COALESCE(resolved_at, '')
		FROM errors WHERE fingerprint = ?
	`, fp).Scan(&d.Type, &d.Value, &d.Level, &d.Stacktrace, &d.Breadcrumbs,
		&d.Release, &d.Environment, &d.UserContext, &d.Tags, &d.Platform,
		&d.FirstSeen, &d.LastSeen, &d.Count, &d.ResolvedAt)
	if err != nil {
		return nil, err
	}
	d.Fingerprint = fp
	d.State = domain.DeriveState(d.ResolvedAt, d.FirstSeen)
	d.TagDist = s.GetTagDistribution(fp)
	return &d, nil
}

// GetTrend returns hourly occurrence counts for an error over the given period.
func (s *Store) GetTrend(fp string, since time.Time) ([]domain.TrendBucket, error) {
	rows, err := s.db.Query(`
		SELECT strftime('%Y-%m-%d %H:00', timestamp) AS hour, COUNT(*) AS cnt
		FROM occurrences
		WHERE fingerprint = ? AND timestamp > ?
		GROUP BY hour ORDER BY hour
	`, fp, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []domain.TrendBucket
	for rows.Next() {
		var b domain.TrendBucket
		if err := rows.Scan(&b.Hour, &b.Count); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

// GetReleases returns occurrence counts grouped by release for an error.
func (s *Store) GetReleases(fp string) ([]domain.ReleaseStats, error) {
	rows, err := s.db.Query(`
		SELECT COALESCE(release_tag, ''), COUNT(*),
			MIN(timestamp), MAX(timestamp)
		FROM occurrences WHERE fingerprint = ?
		GROUP BY release_tag ORDER BY COUNT(*) DESC
	`, fp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var releases []domain.ReleaseStats
	for rows.Next() {
		var r domain.ReleaseStats
		if err := rows.Scan(&r.Release, &r.Count, &r.FirstSeen, &r.LastSeen); err != nil {
			return nil, err
		}
		releases = append(releases, r)
	}
	return releases, rows.Err()
}

// GetStats returns aggregate statistics about stored errors.
func (s *Store) GetStats() (domain.OverviewStats, error) {
	var st domain.OverviewStats
	if err := s.db.QueryRow("SELECT COUNT(*) FROM errors").Scan(&st.UniqueErrors); err != nil {
		return st, err
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM occurrences").Scan(&st.TotalOccurrences); err != nil {
		return st, err
	}
	_ = s.db.QueryRow("SELECT MIN(first_seen) FROM errors").Scan(&st.FirstSeen)
	_ = s.db.QueryRow("SELECT MAX(last_seen) FROM errors").Scan(&st.LastSeen)
	return st, nil
}

// GetCorrelateData returns the error data needed for the correlate view.
func (s *Store) GetCorrelateData(fp string) (*domain.CorrelateData, error) {
	var d domain.CorrelateData
	err := s.db.QueryRow(`
		SELECT type, value, stacktrace, breadcrumbs, user_context
		FROM errors WHERE fingerprint = ?
	`, fp).Scan(&d.Type, &d.Value, &d.Stacktrace, &d.Breadcrumbs, &d.UserContext)
	if err != nil {
		return nil, err
	}
	d.Fingerprint = fp
	return &d, nil
}

// GetNthOccurrence returns the Nth most recent occurrence for an error (1-indexed).
func (s *Store) GetNthOccurrence(fp string, nth int) (*domain.Occurrence, error) {
	var o domain.Occurrence
	err := s.db.QueryRow(`
		SELECT timestamp, COALESCE(trace_id, '')
		FROM occurrences WHERE fingerprint = ?
		ORDER BY timestamp DESC LIMIT 1 OFFSET ?
	`, fp, nth-1).Scan(&o.Timestamp, &o.TraceID)
	if err != nil {
		return nil, err
	}
	return &o, nil
}
