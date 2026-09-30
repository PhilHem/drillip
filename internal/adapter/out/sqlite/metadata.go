package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

const metadataSchema = `CREATE TABLE IF NOT EXISTS drillip_metadata (
	key TEXT PRIMARY KEY, value TEXT NOT NULL
);
INSERT OR IGNORE INTO drillip_metadata VALUES ('backup_format_version', '1');`

func openDatabaseFile(path, mode string) (*sql.DB, error) {
	if path != ":memory:" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		path = filepath.ToSlash(absolute)
		// URI paths need a leading slash, including Windows drive paths.
		if path[0] != '/' {
			path = "/" + path
		}
	}
	query := url.Values{"mode": {mode}, "_pragma": {"busy_timeout(5000)"}}
	u := url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}
	if path == ":memory:" {
		query.Set("mode", "memory")
		u = url.URL{Scheme: "file", Opaque: ":memory:", RawQuery: query.Encode()}
	}
	db, err := sql.Open("sqlite", u.String())
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}

func metadataValue(ctx context.Context, db *sql.DB, key string) (string, error) {
	var exists int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='drillip_metadata'").Scan(&exists); err != nil {
		return "", err
	}
	if exists == 0 {
		return "", nil
	}
	var value string
	err := db.QueryRowContext(ctx, "SELECT value FROM drillip_metadata WHERE key=?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func checkBackupFormat(ctx context.Context, db *sql.DB) error {
	version, err := metadataValue(ctx, db, "backup_format_version")
	if err != nil {
		return err
	}
	if version != "" && version != "1" {
		return fmt.Errorf("unsupported Drillip backup format %q; use a matching Drillip build", version)
	}
	return nil
}

func metadataTime(ctx context.Context, db *sql.DB, key string) (*time.Time, error) {
	value, err := metadataValue(ctx, db, key)
	if err != nil || value == "" {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("invalid database timestamp %s: %w", key, err)
	}
	return &t, nil
}

// DatabaseHistory reads persisted operation history without checking the whole
// database. A generated backup does not imply successful client-side storage.
func (s *Store) DatabaseHistory(ctx context.Context) (domain.DatabaseHistory, error) {
	var history domain.DatabaseHistory
	// One query keeps the related restore timestamps in the same read snapshot.
	var generated, restored, snapshot sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT value FROM drillip_metadata WHERE key='last_backup_generated_at'),
		(SELECT value FROM drillip_metadata WHERE key='last_restored_at'),
		(SELECT value FROM drillip_metadata WHERE key='restored_snapshot_at')`).Scan(&generated, &restored, &snapshot)
	if err != nil {
		return history, err
	}
	for i, value := range []sql.NullString{generated, restored, snapshot} {
		if !value.Valid {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, value.String)
		if err != nil {
			return domain.DatabaseHistory{}, fmt.Errorf("invalid database history: %w", err)
		}
		switch i {
		case 0:
			history.LastBackupGeneratedAt = &t
		case 1:
			history.LastRestoredAt = &t
		case 2:
			history.RestoredSnapshotAt = &t
		}
	}
	return history, nil
}

func stampSnapshot(ctx context.Context, path string, snapshot time.Time) (err error) {
	db, err := openDatabaseFile(path, "rw")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	// The private artifact must remain a single file after metadata is written.
	_, err = db.ExecContext(ctx, `PRAGMA journal_mode=DELETE;
		DELETE FROM drillip_metadata WHERE key IN ('last_backup_generated_at', 'last_restored_at', 'restored_snapshot_at');
		INSERT OR REPLACE INTO drillip_metadata VALUES ('snapshot_at', ?);`, snapshot.Format(time.RFC3339Nano))
	return err
}
