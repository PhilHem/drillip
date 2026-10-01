package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

func databaseRows(t *testing.T, db *sql.DB) string {
	t.Helper()
	// Backup and restore intentionally replace operational metadata. Compare all
	// application tables here; timestamp and format behavior have separate checks.
	rows, err := db.Query("SELECT name FROM sqlite_schema WHERE type='table' AND name!='drillip_metadata' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	result := map[string][][]any{}
	for _, table := range tables {
		rows, err := db.Query(fmt.Sprintf(`SELECT * FROM "%s" ORDER BY rowid`, table))
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			result[table] = append(result[table], values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func saveSnapshot(t *testing.T, backup domain.DatabaseBackup) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "standalone.db")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(file, backup)
	if err != nil {
		t.Fatal(err)
	}
	if n != backup.Size() {
		t.Fatalf("copied %d of %d", n, backup.Size())
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := backup.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBackupPreservesEntireDatabaseAndCleansUp(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	s := setupStore(t)
	var last string
	for i := range 4 {
		result, err := s.StoreEvent(&domain.Event{Message: "backup fixture", Release: fmt.Sprintf("v%d", i), Tags: map[string]string{"service": "checkout"}})
		if err != nil {
			t.Fatal(err)
		}
		last = result.Fingerprint
	}
	if _, err := s.Resolve(last); err != nil {
		t.Fatal(err)
	}
	if err := s.Silence(last, nil, "keep this silence"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE errors SET stacktrace='{"frames":[{"function":"checkout"}]}', breadcrumbs='[{"message":"before failure"}]', user_context='{"id":"operator"}', notified_at='2026-09-30T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	expected := databaseRows(t, s.db)
	backup, err := s.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(context.Background()); !errors.Is(err, domain.ErrBackupBusy) {
		t.Fatalf("second backup: %v", err)
	}
	path := saveSnapshot(t, backup)
	if err := backup.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(temporary)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files: %v, %v", entries, err)
	}
	copy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	if got := databaseRows(t, copy); got != expected {
		t.Fatalf("restored data differs\n%s\n%s", got, expected)
	}
	var check string
	if err := copy.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		t.Fatalf("integrity=%q err=%v", check, err)
	}
	backup, err = s.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := backup.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBackupKeepsSnapshotConsistentWhileWritersCommit(t *testing.T) {
	s := setupStore(t)
	if _, err := s.db.Exec("CREATE TABLE backup_probe (id INTEGER PRIMARY KEY, generation INTEGER, payload BLOB)"); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := range 1024 {
		if _, err := tx.Exec("INSERT INTO backup_probe VALUES (?, 0, zeroblob(4096))", i); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var commits atomic.Int64
	done := make(chan error, 1)
	go func() {
		for ctx.Err() == nil {
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				done <- err
				return
			}
			_, err = tx.ExecContext(ctx, "UPDATE backup_probe SET generation=generation+1 WHERE id IN (0, 1)")
			if err == nil {
				err = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
			if err != nil {
				done <- err
				return
			}
			commits.Add(1)
			time.Sleep(time.Millisecond)
		}
		done <- nil
	}()
	defer func() { cancel(); <-done }()
	backup, err := s.Backup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if commits.Load() == 0 {
		t.Fatal("writer made no progress during the backup")
	}
	path := saveSnapshot(t, backup)
	copy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	rows, err := copy.Query("SELECT generation FROM backup_probe WHERE id IN (0, 1) ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	var generations []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		generations = append(generations, n)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(generations) != 2 || generations[0] != generations[1] {
		t.Fatalf("torn transaction: %v", generations)
	}
}

func TestBackupCancellationAndFailureReleaseResources(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	s := setupStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Backup(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled backup: %v", err)
	}
	conn1, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	conn2, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := s.Backup(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked backup: %v", err)
	}
	_ = conn1.Close()
	_ = conn2.Close()
	entries, err := os.ReadDir(temporary)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed backup files: %v %v", entries, err)
	}
	backup, err := s.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := backup.Close(); err != nil {
		t.Fatal(err)
	}
}
