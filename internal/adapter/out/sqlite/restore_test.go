package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

func restoreFixture(t *testing.T) (*Store, string) {
	t.Helper()
	s := setupStore(t)
	for range 3 {
		event, err := s.StoreEvent(&domain.Event{Message: "restore fixture", Release: "v1", Tags: map[string]string{"service": "checkout"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Silence(event.Fingerprint, nil, "retain this"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("CREATE TABLE future_data (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO future_data VALUES (7, 'keep this too')"); err != nil {
		t.Fatal(err)
	}
	backup, err := s.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s, saveSnapshot(t, backup)
}

func TestRestorePreservesDataAndRecordsNewHistory(t *testing.T) {
	s, input := restoreFixture(t)
	expected := databaseRows(t, s.db)
	history, err := s.DatabaseHistory(context.Background())
	if err != nil || history.LastBackupGeneratedAt == nil || history.LastRestoredAt != nil {
		t.Fatalf("source history=%+v err=%v", history, err)
	}
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(input, 0400); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "restored with spaces.db")
	start := time.Now()
	if err := Restore(context.Background(), input, output); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("restored permissions=%v err=%v", info, err)
	}
	restored, err := Open(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := databaseRows(t, restored.db); got != expected {
		t.Fatalf("restored rows differ\n%s\n%s", got, expected)
	}
	history, err = restored.DatabaseHistory(context.Background())
	if err != nil || history.LastRestoredAt == nil || history.RestoredSnapshotAt == nil || history.LastBackupGeneratedAt != nil {
		t.Fatalf("restored history=%+v err=%v", history, err)
	}
	if history.LastRestoredAt.Before(start) || history.RestoredSnapshotAt.After(*history.LastRestoredAt) {
		t.Fatalf("incorrect history: %+v", history)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err = Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	reopened, err := restored.DatabaseHistory(context.Background())
	if err != nil || !reopened.LastRestoredAt.Equal(*history.LastRestoredAt) || !reopened.RestoredSnapshotAt.Equal(*history.RestoredSnapshotAt) {
		t.Fatalf("history lost after restart: %+v %v", reopened, err)
	}
	after, err := os.ReadFile(input)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("restore changed the backup")
	}
	if _, err := restored.StoreEvent(&domain.Event{Message: "new after restore"}); err != nil {
		t.Fatal(err)
	}
	backup, err := restored.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	next := saveSnapshot(t, backup)
	nextOutput := filepath.Join(t.TempDir(), "again.db")
	if err := Restore(context.Background(), next, nextOutput); err != nil {
		t.Fatal(err)
	}
	again, err := Open(nextOutput)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	nextHistory, err := again.DatabaseHistory(context.Background())
	if err != nil || !nextHistory.LastRestoredAt.After(*history.LastRestoredAt) || !nextHistory.RestoredSnapshotAt.After(*history.RestoredSnapshotAt) {
		t.Fatalf("inherited old restore history: %+v %v", nextHistory, err)
	}
}

func TestRestoreRejectsInvalidInputsAndExistingDestinations(t *testing.T) {
	_, valid := restoreFixture(t)
	for _, failure := range []string{"garbage", "truncated", "unrelated", "schema", "future-format", "bad-time", "existing", "symlink", "target-wal", "source-wal", "missing", "directory", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input.db")
			output := filepath.Join(dir, "new.db")
			data, err := os.ReadFile(valid)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(input, data, 0600); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "garbage":
				os.WriteFile(input, []byte("this is not a database"), 0600)
			case "truncated":
				os.Truncate(input, int64(len(data)/2))
			case "unrelated":
				os.Remove(input)
				db, _ := sql.Open("sqlite", input)
				_, err = db.Exec("CREATE TABLE unrelated (id INTEGER)")
				db.Close()
			case "schema", "future-format", "bad-time":
				db, _ := sql.Open("sqlite", input)
				query := "DROP INDEX idx_occ_trace; ALTER TABLE occurrences DROP COLUMN trace_id"
				if failure == "future-format" {
					query = "UPDATE drillip_metadata SET value='999' WHERE key='backup_format_version'"
				} else if failure == "bad-time" {
					query = "UPDATE drillip_metadata SET value='not a timestamp' WHERE key='snapshot_at'"
				}
				_, err = db.Exec(query)
				db.Close()
			case "existing":
				os.WriteFile(output, []byte("keep me"), 0600)
			case "symlink":
				os.Symlink(input, output)
			case "target-wal":
				os.WriteFile(output+"-wal", []byte("keep me"), 0600)
			case "source-wal":
				os.WriteFile(input+"-wal", []byte("live database"), 0600)
			case "missing":
				os.Remove(input)
			case "directory":
				input = dir
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(input)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "cancelled" {
				cancel()
			}
			err = Restore(ctx, input, output)
			if err == nil {
				t.Fatal("accepted invalid restore")
			}
			if failure == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if failure == "existing" {
				contents, _ := os.ReadFile(output)
				if string(contents) != "keep me" {
					t.Fatal("replaced destination")
				}
			} else if failure != "symlink" {
				if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatal("failed restore published a database")
				}
			}
			after, _ := os.ReadFile(input)
			if !bytes.Equal(before, after) {
				t.Fatal("failed restore changed the input")
			}
			staged, _ := filepath.Glob(filepath.Join(dir, ".drillip-restore-*"))
			if len(staged) != 0 {
				t.Fatalf("temporary files remain: %v", staged)
			}
		})
	}
}

func TestRestoreHandlesCancellationAndDestinationRaceDuringCopy(t *testing.T) {
	_, input := restoreFixture(t)
	db, err := sql.Open("sqlite", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE large_payload (value BLOB); INSERT INTO large_payload VALUES (zeroblob(33554432))"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for _, operation := range []string{"cancel", "create-destination"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "restored.db")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				for ctx.Err() == nil {
					files, err := filepath.Glob(filepath.Join(dir, ".drillip-restore-*"))
					if err != nil {
						done <- err
						return
					}
					if len(files) > 0 {
						if operation == "cancel" {
							cancel()
							done <- nil
						} else {
							done <- os.WriteFile(output, []byte("concurrent destination"), 0600)
						}
						return
					}
					runtime.Gosched()
				}
				done <- ctx.Err()
			}()
			err := Restore(ctx, input, output)
			cancel()
			if observerErr := <-done; observerErr != nil {
				t.Fatal(observerErr)
			}
			if err == nil {
				t.Fatal("restore ignored cancellation or replaced a concurrent destination")
			}
			if operation == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("cancelled restore published a file")
				}
			} else if data, _ := os.ReadFile(output); string(data) != "concurrent destination" {
				t.Fatal("restore replaced concurrent destination")
			}
			files, _ := filepath.Glob(filepath.Join(dir, ".drillip-restore-*"))
			if len(files) != 0 {
				t.Fatalf("restore left temporary files: %v", files)
			}
		})
	}
}

func TestRestoreAcceptsLegacyBackupWithUnknownSnapshotTime(t *testing.T) {
	_, input := restoreFixture(t)
	db, err := sql.Open("sqlite", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE drillip_metadata"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	output := filepath.Join(t.TempDir(), "legacy.db")
	if err := Restore(context.Background(), input, output); err != nil {
		t.Fatal(err)
	}
	s, err := Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	history, err := s.DatabaseHistory(context.Background())
	if err != nil || history.LastRestoredAt == nil || history.RestoredSnapshotAt != nil {
		t.Fatalf("legacy history=%+v err=%v", history, err)
	}
}

type restoreReaderFunc func([]byte) (int, error)

func (read restoreReaderFunc) Read(p []byte) (int, error) { return read(p) }

func TestRestoreFromReaderWaitsForEOFAndPreservesAllData(t *testing.T) {
	s, input := restoreFixture(t)
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = reader.CloseWithError(ctx.Err()) })
	defer stop()
	output := filepath.Join(t.TempDir(), "stream.db")
	done := make(chan error, 1)
	go func() { done <- RestoreFromReader(ctx, reader, output) }()
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("restore finished before EOF: %v", err)
	default:
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("restore published before EOF")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v err=%v", info, err)
	}
	restored, err := Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if got, want := databaseRows(t, restored.db), databaseRows(t, s.db); got != want {
		t.Fatalf("stream restore lost rows\ngot: %s\nwant: %s", got, want)
	}
	history, err := restored.DatabaseHistory(ctx)
	if err != nil || history.LastRestoredAt == nil || history.RestoredSnapshotAt == nil || history.LastBackupGeneratedAt != nil {
		t.Fatalf("history=%+v err=%v", history, err)
	}
}

func TestRestoreFromReaderRejectsFailedStreamsWithoutPublishing(t *testing.T) {
	_, input := restoreFixture(t)
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	readErr := errors.New("backup transfer failed")
	for _, failure := range []string{"empty", "invalid", "truncated", "read-error", "cancel-at-eof", "destination-race"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "restored.db")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var source io.Reader
			switch failure {
			case "empty":
				source = strings.NewReader("")
			case "invalid":
				source = strings.NewReader("not a backup")
			case "truncated":
				source = bytes.NewReader(data[:len(data)/2])
			case "read-error":
				source = io.MultiReader(bytes.NewReader(data), restoreReaderFunc(func([]byte) (int, error) { return 0, readErr }))
			case "cancel-at-eof":
				source = io.MultiReader(bytes.NewReader(data), restoreReaderFunc(func([]byte) (int, error) {
					cancel()
					return 0, io.EOF
				}))
			case "destination-race":
				source = io.MultiReader(bytes.NewReader(data), restoreReaderFunc(func([]byte) (int, error) {
					if err := os.WriteFile(output, []byte("keep concurrent file"), 0600); err != nil {
						return 0, err
					}
					return 0, io.EOF
				}))
			}
			err := RestoreFromReader(ctx, source, output)
			if err == nil {
				t.Fatal("accepted failed stream")
			}
			if failure == "read-error" && !errors.Is(err, readErr) {
				t.Fatal(err)
			}
			if failure == "cancel-at-eof" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if failure == "destination-race" {
				if got, _ := os.ReadFile(output); string(got) != "keep concurrent file" {
					t.Fatal("replaced concurrent destination")
				}
			} else if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("failed stream published a database")
			}
			staged, err := filepath.Glob(filepath.Join(dir, ".drillip-restore-*"))
			if err != nil || len(staged) != 0 {
				t.Fatalf("temporary files=%v err=%v", staged, err)
			}
		})
	}
}

func TestRestoreFromReaderRejectsExistingDestinationBeforeReading(t *testing.T) {
	output := filepath.Join(t.TempDir(), "existing.db")
	if err := os.WriteFile(output, []byte("keep existing file"), 0600); err != nil {
		t.Fatal(err)
	}
	read := false
	source := restoreReaderFunc(func([]byte) (int, error) { read = true; return 0, io.EOF })
	if err := RestoreFromReader(context.Background(), source, output); err == nil || read {
		t.Fatalf("err=%v read=%v", err, read)
	}
	if got, _ := os.ReadFile(output); string(got) != "keep existing file" {
		t.Fatal("changed destination")
	}
}
