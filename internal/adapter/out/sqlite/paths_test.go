package sqlite

import (
	"context"
	"os"
	"testing"

	"github.com/PhilHem/drillip/internal/domain"
)

func TestDatabaseAndRestoreAcceptRelativePathsWithURICharacters(t *testing.T) {
	t.Chdir(t.TempDir())
	s, err := Open("source ?#.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.StoreEvent(&domain.Event{Message: "relative paths"}); err != nil {
		t.Fatal(err)
	}
	backup, err := s.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Create("backup ?#.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.ReadFrom(backup); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := backup.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Restore(context.Background(), "backup ?#.db", "restored ?#.db"); err != nil {
		t.Fatal(err)
	}
	restored, err := Open("restored ?#.db")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var occurrences int
	if err := restored.db.QueryRow("SELECT count(*) FROM occurrences").Scan(&occurrences); err != nil || occurrences != 1 {
		t.Fatalf("occurrences=%d err=%v", occurrences, err)
	}
}
