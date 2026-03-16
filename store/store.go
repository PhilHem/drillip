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
	DB *sql.DB
}

// Open creates a new Store backed by the SQLite database at path.
func Open(path string) (*Store, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

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
	`)
	if err != nil {
		sqlDB.Close()
		return nil, err
	}

	s := &Store{DB: sqlDB}
	if err := s.migrateDB(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.DB.Close()
}

// Checkpoint runs a WAL checkpoint.
func (s *Store) Checkpoint() error {
	_, err := s.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
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
	}

	for _, m := range migrations {
		if s.columnExists(m.table, m.column) {
			continue
		}
		if _, err := s.DB.Exec(m.ddl); err != nil {
			return fmt.Errorf("migrate %s.%s: %w", m.table, m.column, err)
		}
	}
	return nil
}

func (s *Store) columnExists(table, column string) bool {
	rows, err := s.DB.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
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
		evValue = ev.MessageText()
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

	tx, err := s.DB.Begin()
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

// AutoResolve marks errors as resolved if they haven't been seen within olderThan.
func (s *Store) AutoResolve(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339)
	res, err := s.DB.Exec(
		`UPDATE errors SET resolved_at = last_seen WHERE resolved_at IS NULL AND last_seen < ?`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Resolve manually marks an error as resolved by fingerprint prefix.
// Returns the number of rows affected.
func (s *Store) Resolve(fpPrefix string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.DB.Exec(
		`UPDATE errors SET resolved_at = ? WHERE fingerprint LIKE ?||'%' AND resolved_at IS NULL`,
		now, fpPrefix,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
