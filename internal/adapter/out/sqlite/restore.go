package sqlite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Restore installs a checked, standalone Drillip backup into a new database.
// It never opens or changes the source through SQLite, starts a server, or
// replaces an existing destination. Publication follows verification and sync.
func Restore(ctx context.Context, input, destination string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(input + suffix); err == nil {
			return fmt.Errorf("restore requires a standalone backup; %s exists", input+suffix)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	info, err := os.Stat(input)
	if err != nil {
		return fmt.Errorf("check restore input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("restore input must be a regular backup file")
	}
	source, err := os.Open(input)
	if err != nil {
		return fmt.Errorf("open restore input: %w", err)
	}
	defer source.Close()
	info, err = source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("restore input must be a regular backup file")
	}
	return RestoreFromReader(ctx, source, destination)
}

// RestoreFromReader checks a complete backup stream and publishes a new database.
// It reads through EOF before verification and never replaces the destination.
// The caller owns the reader and must unblock a pending read on cancellation.
func RestoreFromReader(ctx context.Context, source io.Reader, destination string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if source == nil {
		return fmt.Errorf("restore input is unavailable")
	}
	for _, path := range []string{destination, destination + "-wal", destination + "-shm", destination + "-journal"} {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("restore destination already exists: %s; choose a new database path", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	stage, err := os.CreateTemp(filepath.Dir(destination), ".drillip-restore-*")
	if err != nil {
		return err
	}
	defer stage.Close()
	defer func() {
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			if removeErr := os.Remove(stage.Name() + suffix); removeErr != nil && !os.IsNotExist(removeErr) {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	if _, err := io.Copy(stage, contextReader{ctx: ctx, reader: source}); err != nil {
		return fmt.Errorf("copy restore input: %w", err)
	}
	// Verify the exact staged bytes; the source remains untouched, even if the
	// backup is invalid. Read-only mode cannot create missing schema or migrate it.
	snapshot, err := verifyRestore(ctx, stage.Name())
	if err != nil {
		return fmt.Errorf("check restore input: %w", err)
	}
	if err := stampRestore(ctx, stage.Name(), snapshot); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	// Link, rather than rename, also refuses a destination created during restore.
	if err := os.Link(stage.Name(), destination); err != nil {
		return fmt.Errorf("publish restored database without replacing an existing file: %w", err)
	}
	return nil
}

func verifyRestore(ctx context.Context, path string) (_ *time.Time, err error) {
	db, err := openDatabaseFile(path, "ro")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return nil, err
	}
	if integrity != "ok" {
		return nil, fmt.Errorf("database integrity check failed: %s", integrity)
	}
	var tables int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name IN ('errors', 'occurrences', 'silences')").Scan(&tables); err != nil || tables != 3 {
		return nil, fmt.Errorf("backup does not contain the Drillip database tables")
	}
	// Additive columns from old builds can be migrated by Open. These are the
	// base columns required to identify a compatible Drillip database.
	for _, query := range []string{
		"SELECT id,fingerprint,type,value,stacktrace,breadcrumbs,release_tag,environment,user_context,tags,platform,first_seen,last_seen,count FROM errors LIMIT 0",
		"SELECT id,fingerprint,timestamp,release_tag,trace_id FROM occurrences LIMIT 0",
		"SELECT id,fingerprint,created_at,expires_at,reason FROM silences LIMIT 0",
	} {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("incompatible Drillip database schema: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	if err := checkBackupFormat(ctx, db); err != nil {
		return nil, err
	}
	return metadataTime(ctx, db, "snapshot_at")
}

func stampRestore(ctx context.Context, path string, snapshot *time.Time) (err error) {
	db, err := openDatabaseFile(path, "rw")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=DELETE;"+metadataSchema); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM drillip_metadata WHERE key IN ('snapshot_at', 'last_backup_generated_at', 'last_restored_at', 'restored_snapshot_at')"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO drillip_metadata VALUES ('last_restored_at', ?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if snapshot != nil {
		if _, err := tx.ExecContext(ctx, "INSERT INTO drillip_metadata VALUES ('restored_snapshot_at', ?)", snapshot.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if ctxErr := r.ctx.Err(); ctxErr != nil {
		return n, ctxErr
	}
	return n, err
}
