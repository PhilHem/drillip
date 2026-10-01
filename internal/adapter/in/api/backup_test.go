package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

type backupProvider struct {
	backup domain.DatabaseBackup
	err    error
	calls  int
}

func (p *backupProvider) Backup(context.Context) (domain.DatabaseBackup, error) {
	p.calls++
	return p.backup, p.err
}

type testBackup struct {
	*bytes.Reader
	size   int64
	closed bool
}

func (b *testBackup) Size() int64  { return b.size }
func (b *testBackup) Close() error { b.closed = true; return nil }

func TestBackupDownloadHeadersAndCleanup(t *testing.T) {
	data := []byte("SQLite format 3\x00" + string(bytes.Repeat([]byte{0}, 84)))
	backup := &testBackup{Reader: bytes.NewReader(data), size: int64(len(data))}
	provider := &backupProvider{backup: backup}
	w := httptest.NewRecorder()
	(&Handler{Backups: provider}).Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/0/backup/", nil))
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) || !backup.closed {
		t.Fatalf("status=%d closed=%v body=%q", w.Code, backup.closed, w.Body.Bytes())
	}
	for key, value := range map[string]string{"Content-Type": "application/octet-stream", "Content-Length": strconv.Itoa(len(data)), "Cache-Control": "no-store", "Content-Disposition": `attachment; filename="drillip-backup.db"`} {
		if got := w.Header().Get(key); got != value {
			t.Errorf("%s=%q want %q", key, got, value)
		}
	}
}

func TestBackupRejectsWrongMethodAndReportsFailure(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodHead} {
		provider := &backupProvider{}
		w := httptest.NewRecorder()
		(&Handler{Backups: provider}).HandleBackup(w, httptest.NewRequest(method, "/api/0/backup/", nil))
		if w.Code != 405 || provider.calls != 0 || w.Header().Get("Allow") != "GET" {
			t.Fatalf("%s: status=%d calls=%d", method, w.Code, provider.calls)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{domain.ErrBackupBusy, 503}, {errors.New("private database filename"), 500}} {
		w := httptest.NewRecorder()
		(&Handler{Backups: &backupProvider{err: tc.err}}).HandleBackup(w, httptest.NewRequest(http.MethodGet, "/api/0/backup/", nil))
		if w.Code != tc.status || w.Header().Get("Content-Type") != "application/json" || bytes.Contains(w.Body.Bytes(), []byte("private database")) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
		if tc.status == 503 && w.Header().Get("Retry-After") != "5" {
			t.Fatal("busy response omits retry guidance")
		}
	}
}

type failedBackupWriter struct {
	header    http.Header
	deadlines []time.Time
}

func (w *failedBackupWriter) Header() http.Header       { return w.header }
func (w *failedBackupWriter) WriteHeader(int)           {}
func (w *failedBackupWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (w *failedBackupWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func TestBackupAbortedDownloadClosesSnapshotAndResetsDeadline(t *testing.T) {
	backup := &testBackup{Reader: bytes.NewReader([]byte("data")), size: 4}
	w := &failedBackupWriter{header: make(http.Header)}
	(&Handler{Backups: &backupProvider{backup: backup}}).HandleBackup(w, httptest.NewRequest(http.MethodGet, "/api/0/backup/", nil))
	if !backup.closed || len(w.deadlines) != 2 || w.deadlines[0].IsZero() || !w.deadlines[1].IsZero() {
		t.Fatalf("closed=%v deadlines=%v", backup.closed, w.deadlines)
	}
}
