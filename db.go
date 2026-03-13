package main

import (
	"database/sql"

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
			trace_id    TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_occ_fp_ts ON occurrences(fingerprint, timestamp);
		CREATE INDEX IF NOT EXISTS idx_occ_ts ON occurrences(timestamp);
		CREATE INDEX IF NOT EXISTS idx_occ_trace ON occurrences(trace_id);
	`)
	return err
}
