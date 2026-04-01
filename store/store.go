package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PhilHem/drillip/domain"
	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database connection.
type Store struct {
	db *sql.DB
}

// RawDB returns the underlying *sql.DB.
// It exists for test fixture setup in external packages.
func (s *Store) RawDB() *sql.DB {
	return s.db
}

// Open creates a new Store backed by the SQLite database at path.
func Open(path string) (*Store, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(2) // SQLite WAL allows 1 writer + concurrent readers, keep pool small
	sqlDB.SetMaxIdleConns(2)

	_, err = sqlDB.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA synchronous=NORMAL;
		PRAGMA busy_timeout=5000;

		CREATE TABLE IF NOT EXISTS errors (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			fingerprint  TEXT UNIQUE,
			type         TEXT,
			value        TEXT,
			level        TEXT DEFAULT 'error',
			stacktrace   TEXT,
			breadcrumbs  TEXT,
			release_tag  TEXT,
			environment  TEXT,
			user_context TEXT,
			tags         TEXT,
			platform     TEXT,
			first_seen   TEXT,
			last_seen    TEXT,
			count        INTEGER DEFAULT 1
		);

		CREATE INDEX IF NOT EXISTS idx_errors_last_seen ON errors(last_seen);
		CREATE INDEX IF NOT EXISTS idx_errors_type ON errors(type);
		CREATE INDEX IF NOT EXISTS idx_errors_release ON errors(release_tag);
		CREATE INDEX IF NOT EXISTS idx_errors_count ON errors(count);
		CREATE INDEX IF NOT EXISTS idx_errors_level ON errors(level);

		CREATE TABLE IF NOT EXISTS occurrences (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			fingerprint TEXT NOT NULL,
			timestamp   TEXT NOT NULL,
			release_tag TEXT,
			trace_id    TEXT,
			tags        TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_occ_fp_ts ON occurrences(fingerprint, timestamp);
		CREATE INDEX IF NOT EXISTS idx_occ_ts ON occurrences(timestamp);
		CREATE INDEX IF NOT EXISTS idx_occ_trace ON occurrences(trace_id);

		CREATE TABLE IF NOT EXISTS silences (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			fingerprint TEXT NOT NULL,
			created_at  TEXT NOT NULL,
			expires_at  TEXT,
			reason      TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_silences_fp ON silences(fingerprint);
	`)
	if err != nil {
		sqlDB.Close()
		return nil, err
	}

	s := &Store{db: sqlDB}
	if err := s.migrateDB(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Checkpoint runs a WAL checkpoint.
func (s *Store) Checkpoint() error {
	_, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

// migrateDB adds columns that may be missing from older schema versions.
// SQLite's ADD COLUMN is safe and idempotent (we check before adding).
func (s *Store) migrateDB() error {
	migrations := []struct {
		table  string
		column string
		ddl    string
	}{
		{"errors", "level", `ALTER TABLE errors ADD COLUMN level TEXT DEFAULT 'error'`},
		{"occurrences", "tags", `ALTER TABLE occurrences ADD COLUMN tags TEXT`},
		{"errors", "resolved_at", `ALTER TABLE errors ADD COLUMN resolved_at TEXT`},
		{"errors", "notified_at", `ALTER TABLE errors ADD COLUMN notified_at TEXT`},
	}

	for _, m := range migrations {
		if s.columnExists(m.table, m.column) {
			continue
		}
		if _, err := s.db.Exec(m.ddl); err != nil {
			return fmt.Errorf("migrate %s.%s: %w", m.table, m.column, err)
		}
	}
	return nil
}

func (s *Store) columnExists(table, column string) bool {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			continue
		}
		if name == column {
			return true
		}
	}
	return false
}

// StoreResult holds the outcome of storing an event.
type StoreResult struct {
	Fingerprint      string
	IsNew            bool
	IsRegression     bool          // was resolved, now reappeared
	ResolvedDuration time.Duration // how long it was resolved before regressing
}

// StoreEvent persists a domain event into the database.
func (s *Store) StoreEvent(ev *domain.Event) (StoreResult, error) {
	fp := domain.Fingerprint(ev)
	now := time.Now().UTC().Format(time.RFC3339)
	level := ev.EffectiveLevel()

	var evType, evValue, stacktraceJSON string

	if ev.Exception != nil && len(ev.Exception.Values) > 0 {
		exc := ev.Exception.Values[0]
		evType = exc.Type
		evValue = exc.Value
		if b, err := json.Marshal(exc.Stacktrace); err == nil {
			stacktraceJSON = string(b)
		}
	} else {
		evType = "message"
		evValue = domain.StripLogPrefix(ev.MessageText())
	}

	var breadcrumbsJSON string
	if ev.Breadcrumbs != nil {
		if b, err := json.Marshal(ev.Breadcrumbs.Values); err == nil {
			breadcrumbsJSON = string(b)
		}
	}

	userJSON := string(ev.User)

	var tagsJSON string
	if ev.Tags != nil {
		if b, err := json.Marshal(ev.Tags); err == nil {
			tagsJSON = string(b)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return StoreResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// Check if this fingerprint already exists and whether it was resolved
	var isNew, isRegression bool
	var resolvedDuration time.Duration
	var existingResolvedAt sql.NullString
	err = tx.QueryRow("SELECT resolved_at FROM errors WHERE fingerprint = ?", fp).Scan(&existingResolvedAt)
	if err == sql.ErrNoRows {
		isNew = true
	} else if err != nil {
		return StoreResult{}, err
	} else if existingResolvedAt.Valid && existingResolvedAt.String != "" {
		isRegression = true
		if t, parseErr := time.Parse(time.RFC3339, existingResolvedAt.String); parseErr == nil {
			resolvedDuration = time.Since(t)
		}
	}

	_, err = tx.Exec(`
		INSERT INTO errors (fingerprint, type, value, level, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO UPDATE SET
			last_seen = ?,
			count = count + 1,
			breadcrumbs = ?,
			user_context = ?,
			release_tag = COALESCE(?, release_tag),
			resolved_at = NULL
	`, fp, evType, evValue, level, stacktraceJSON, breadcrumbsJSON,
		ev.Release, ev.Environment, userJSON, tagsJSON, ev.Platform, now, now,
		now, breadcrumbsJSON, userJSON, ev.Release)
	if err != nil {
		return StoreResult{}, err
	}

	_, err = tx.Exec(`INSERT INTO occurrences (fingerprint, timestamp, release_tag, trace_id, tags) VALUES (?,?,?,?,?)`,
		fp, now, ev.Release, ev.TraceID(), tagsJSON)
	if err != nil {
		return StoreResult{}, err
	}

	return StoreResult{Fingerprint: fp, IsNew: isNew, IsRegression: isRegression, ResolvedDuration: resolvedDuration}, tx.Commit()
}

// MarkNotified records that a notification was sent for the given fingerprint.
// This is used to filter resolved emails — only errors that were actually
// communicated to the user will appear in resolved digests.
func (s *Store) MarkNotified(fp string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE errors SET notified_at = ? WHERE fingerprint = ?`, now, fp)
	return err
}

// ResolvedError holds metadata about a single error that was resolved.
type ResolvedError struct {
	Fingerprint string
	Type        string
	Value       string
	Level       string
	Count       int
	FirstSeen   string
	LastSeen    string
	ResolvedAt  string
}

// AutoResolve marks errors as resolved if they haven't been seen within olderThan.
// Returns the list of errors that were resolved and any error encountered.
func (s *Store) AutoResolve(olderThan time.Duration) ([]ResolvedError, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339)
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Collect details of errors about to be resolved — only include
	// errors that were previously notified about, so the resolved email
	// doesn't contain noise the user never heard about.
	rows, err := tx.Query(
		`SELECT fingerprint, type, value, level, count, first_seen, last_seen FROM errors WHERE resolved_at IS NULL AND last_seen < ? AND notified_at IS NOT NULL`,
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	var resolved []ResolvedError
	for rows.Next() {
		var r ResolvedError
		if err := rows.Scan(&r.Fingerprint, &r.Type, &r.Value, &r.Level, &r.Count, &r.FirstSeen, &r.LastSeen); err != nil {
			rows.Close()
			return nil, err
		}
		r.ResolvedAt = now
		resolved = append(resolved, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Mark ALL stale errors as resolved (not just notified ones).
	_, err = tx.Exec(
		`UPDATE errors SET resolved_at = ? WHERE resolved_at IS NULL AND last_seen < ?`,
		now, cutoff,
	)
	if err != nil {
		return nil, err
	}

	return resolved, tx.Commit()
}

// GCOccurrences deletes occurrence rows older than the given threshold.
func (s *Store) GCOccurrences(before time.Time) (int64, error) {
	res, err := s.db.Exec("DELETE FROM occurrences WHERE timestamp < ?", before.Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SilenceEntry represents an active silence rule.
type SilenceEntry struct {
	Fingerprint string
	CreatedAt   string
	ExpiresAt   string // empty if permanent
	Reason      string
}

// Silence mutes notifications for a fingerprint. If expiresAt is nil, the silence is permanent.
func (s *Store) Silence(fp string, expiresAt *time.Time, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var exp interface{}
	if expiresAt != nil {
		exp = expiresAt.UTC().Format(time.RFC3339)
	}
	_, err := s.db.Exec(
		`INSERT INTO silences (fingerprint, created_at, expires_at, reason) VALUES (?, ?, ?, ?)`,
		fp, now, exp, reason,
	)
	return err
}

// Unsilence removes all silences for a fingerprint.
func (s *Store) Unsilence(fp string) error {
	_, err := s.db.Exec(`DELETE FROM silences WHERE fingerprint = ?`, fp)
	return err
}

// ListSilences returns all active (non-expired) silences.
func (s *Store) ListSilences() ([]SilenceEntry, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := s.db.Query(
		`SELECT fingerprint, created_at, COALESCE(expires_at, ''), COALESCE(reason, '')
		 FROM silences
		 WHERE expires_at IS NULL OR expires_at > ?`, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []SilenceEntry
	for rows.Next() {
		var e SilenceEntry
		if err := rows.Scan(&e.Fingerprint, &e.CreatedAt, &e.ExpiresAt, &e.Reason); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// IsSilenced checks whether a fingerprint is currently silenced.
func (s *Store) IsSilenced(fp string) bool {
	now := time.Now().UTC().Format(time.RFC3339)
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM silences WHERE fingerprint = ? AND (expires_at IS NULL OR expires_at > ?)`,
		fp, now,
	).Scan(&count)
	return err == nil && count > 0
}

// PruneExpiredSilences removes silences that have passed their expiry time.
func (s *Store) PruneExpiredSilences() (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`DELETE FROM silences WHERE expires_at IS NOT NULL AND expires_at < ?`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ResolveResult holds the outcome of a manual resolve operation.
type ResolveResult struct {
	Matched     int64
	Fingerprint string // full fingerprint of the first matched error
	ResolvedAt  string
	Resolved    []ResolvedError // details of resolved errors
}

// Resolve manually marks an error as resolved by fingerprint prefix.
func (s *Store) Resolve(fpPrefix string) (ResolveResult, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return ResolveResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// Collect details before resolving
	rows, err := tx.Query(
		`SELECT fingerprint, type, value, level, count, first_seen, last_seen FROM errors WHERE fingerprint LIKE ?||'%' AND resolved_at IS NULL`,
		fpPrefix,
	)
	if err != nil {
		return ResolveResult{}, err
	}
	var resolved []ResolvedError
	for rows.Next() {
		var r ResolvedError
		if err := rows.Scan(&r.Fingerprint, &r.Type, &r.Value, &r.Level, &r.Count, &r.FirstSeen, &r.LastSeen); err != nil {
			rows.Close()
			return ResolveResult{}, err
		}
		r.ResolvedAt = now
		resolved = append(resolved, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ResolveResult{}, err
	}

	if len(resolved) == 0 {
		if err := tx.Commit(); err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{Matched: 0, ResolvedAt: now}, nil
	}

	res, err := tx.Exec(
		`UPDATE errors SET resolved_at = ? WHERE fingerprint LIKE ?||'%' AND resolved_at IS NULL`,
		now, fpPrefix,
	)
	if err != nil {
		return ResolveResult{}, err
	}
	n, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		return ResolveResult{}, err
	}

	return ResolveResult{Matched: n, Fingerprint: resolved[0].Fingerprint, ResolvedAt: now, Resolved: resolved}, nil
}

// TagValue holds a single tag value and its occurrence count/percentage.
type TagValue struct {
	Value   string `json:"value"`
	Count   int    `json:"count"`
	Percent int    `json:"percent"`
}

// TagDist holds the distribution of values for a single tag key.
type TagDist struct {
	Values []TagValue `json:"values"`
}

// GetTagDistribution returns the distribution of tag values across occurrences
// for the given fingerprint.
func (s *Store) GetTagDistribution(fp string) map[string]TagDist {
	rows, err := s.db.Query(`SELECT tags FROM occurrences WHERE fingerprint = ? AND tags != '' AND tags IS NOT NULL`, fp)
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
	if err := rows.Err(); err != nil {
		return nil
	}

	if len(dist) == 0 {
		return nil
	}

	result := map[string]TagDist{}
	for k, values := range dist {
		var td TagDist
		for v, c := range values {
			pct := 0
			if total > 0 {
				pct = c * 100 / total
			}
			td.Values = append(td.Values, TagValue{Value: v, Count: c, Percent: pct})
		}
		result[k] = td
	}
	return result
}

// FindByPrefix resolves a fingerprint prefix to the full fingerprint.
func (s *Store) FindByPrefix(prefix string) (string, error) {
	var fullFP string
	err := s.db.QueryRow("SELECT fingerprint FROM errors WHERE fingerprint LIKE ?||'%' LIMIT 1", prefix).Scan(&fullFP)
	if err != nil {
		return "", fmt.Errorf("not found: %s", prefix)
	}
	return fullFP, nil
}
