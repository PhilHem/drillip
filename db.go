package main

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func initDB(path string) error {
	var err error
	db, err = sql.Open("sqlite", path)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
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
		return err
	}

	return migrateDB()
}

// migrateDB adds columns that may be missing from older schema versions.
// SQLite's ADD COLUMN is safe and idempotent (we check before adding).
func migrateDB() error {
	migrations := []struct {
		table  string
		column string
		ddl    string
	}{
		{"errors", "level", `ALTER TABLE errors ADD COLUMN level TEXT DEFAULT 'error'`},
		{"occurrences", "tags", `ALTER TABLE occurrences ADD COLUMN tags TEXT`},
	}

	for _, m := range migrations {
		if columnExists(m.table, m.column) {
			continue
		}
		if _, err := db.Exec(m.ddl); err != nil {
			return fmt.Errorf("migrate %s.%s: %w", m.table, m.column, err)
		}
	}
	return nil
}

func columnExists(table, column string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
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
