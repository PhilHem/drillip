package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
	driver "modernc.org/sqlite"
	sqlitecode "modernc.org/sqlite/lib"
)

// Backup owns SQLite consistency, verification, temporary files, and admission.
// The slot stays occupied until the caller closes the snapshot, bounding disk
// usage even when a download is slow.
func (s *Store) Backup(ctx context.Context) (_ domain.DatabaseBackup, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case s.backupGate <- struct{}{}:
	default:
		return nil, domain.ErrBackupBusy
	}
	dir, err := os.MkdirTemp("", "drillip-backup-")
	if err != nil {
		<-s.backupGate
		return nil, fmt.Errorf("create backup directory: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			err = errors.Join(err, os.RemoveAll(dir))
			<-s.backupGate
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	path := filepath.Join(dir, "backup.db")
	if err := s.copyDatabase(ctx, path); err != nil {
		return nil, fmt.Errorf("create database snapshot: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	if err := checkBackup(ctx, path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	complete = true
	return &databaseBackup{File: file, size: info.Size(), dir: dir, gate: s.backupGate}, nil
}

func (s *Store) copyDatabase(ctx context.Context, path string) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Pin a WAL read snapshot. Other connections can keep committing writes,
	// without restarting an incremental backup on every new commit.
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var tables int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema").Scan(&tables); err != nil {
		return err
	}
	return conn.Raw(func(raw any) (err error) {
		backuper, ok := raw.(interface {
			NewBackup(string) (*driver.Backup, error)
		})
		if !ok {
			return fmt.Errorf("SQLite driver does not support online backups")
		}
		backup, err := backuper.NewBackup(path)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, backup.Finish()) }()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := backup.Step(128)
			if err == nil && !more {
				return ctx.Err()
			}
			if err != nil {
				var sqliteErr *driver.Error
				if !errors.As(err, &sqliteErr) || (sqliteErr.Code()&0xff != sqlitecode.SQLITE_BUSY && sqliteErr.Code()&0xff != sqlitecode.SQLITE_LOCKED) {
					return err
				}
			}
			// Yield between batches and retry temporary SQLite locks. The context
			// bounds the operation while other connections continue writing.
			timer := time.NewTimer(time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	})
}

func checkBackup(ctx context.Context, path string) (err error) {
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return fmt.Errorf("verify database backup: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("database backup failed its integrity check: %s", result)
	}
	return nil
}

type databaseBackup struct {
	*os.File
	size     int64
	dir      string
	gate     chan struct{}
	once     sync.Once
	closeErr error
}

func (b *databaseBackup) Size() int64 { return b.size }

func (b *databaseBackup) Close() error {
	b.once.Do(func() {
		b.closeErr = errors.Join(b.File.Close(), os.RemoveAll(b.dir))
		<-b.gate
	})
	return b.closeErr
}
