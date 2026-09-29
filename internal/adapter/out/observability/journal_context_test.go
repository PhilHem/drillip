package observability

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJournalctlUsesAbsoluteTimesAndStopsOnCancellation(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires sh")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "journalctl")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$JOURNAL_ARGS\"\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(dir, "args")
	t.Setenv("JOURNAL_ARGS", argsFile)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	at := time.Unix(1234567890, 0).In(time.FixedZone("offset", 7200))
	start := time.Now()
	_, err := QueryJournalctl(ctx, "app.service", at)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("err=%v elapsed=%v", err, time.Since(start))
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@1234567885", "@1234567895"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("arguments=%s", data)
		}
	}
}
