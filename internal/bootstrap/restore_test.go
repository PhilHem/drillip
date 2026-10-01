package bootstrap

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/PhilHem/drillip/internal/adapter/out/sqlite"
	"github.com/PhilHem/drillip/internal/domain"
)

func TestRestoreCLIUsesLocalFilesAndImageDatabaseDefault(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.StoreEvent(&domain.Event{Message: "CLI restore"}); err != nil {
		t.Fatal(err)
	}
	backup, err := s.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "backup.db")
	file, _ := os.Create(input)
	if _, err := file.ReadFrom(backup); err != nil {
		t.Fatal(err)
	}
	file.Close()
	backup.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("local restore contacted the server")
	}))
	defer srv.Close()
	t.Setenv("DRILLIP_SERVER", srv.URL)
	t.Setenv("DRILLIP_SMTP_PORT", "not a port")
	t.Setenv("DRILLIP_VM_URL", "not a URL")
	output := filepath.Join(t.TempDir(), "restored.db")
	t.Setenv("DRILLIP_DB", output)
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"restore", "--input", input}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "restored "+output+"\n" {
		t.Fatal(&stdout)
	}
	restored, err := store.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	history, err := restored.DatabaseHistory(context.Background())
	if err != nil || history.LastRestoredAt == nil || history.RestoredSnapshotAt == nil {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"restore", "--input", input}, &stdout, &stderr); err == nil || stdout.Len() != 0 {
		t.Fatal("restore accepted an existing database")
	}
}

func TestRestoreCLIValidatesBeforeAccess(t *testing.T) {
	t.Setenv("DRILLIP_DB", "")
	for _, args := range [][]string{{"restore"}, {"restore", "--input", "missing"}, {"restore", "--db", "new.db"}, {"restore", "--input", "missing", "--db", "new.db", "extra"}, {"--server", "http://localhost", "restore", "--input", "missing", "--db", "new.db"}} {
		var out, errOut bytes.Buffer
		if err := Run(context.Background(), args, &out, &errOut); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), []string{"restore", "--help"}, &out, &errOut); err != nil || !strings.Contains(errOut.String(), "-input") {
		t.Fatalf("help err=%v output=%s", err, &errOut)
	}
}
