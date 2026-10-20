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
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$JOURNAL_ARGS.tmp\"\nmv \"$JOURNAL_ARGS.tmp\" \"$JOURNAL_ARGS\"\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(dir, "args")
	t.Setenv("JOURNAL_ARGS", argsFile)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancel only after journalctl has written its arguments; the script renames the file, so it appears complete.
	cancelled := make(chan time.Time, 1)
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(argsFile); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		cancelled <- time.Now()
		cancel()
	}()
	at := time.Unix(1234567890, 0).In(time.FixedZone("offset", 7200))
	_, err := QueryJournalctl(ctx, "app.service", at)
	stopped := time.Now()
	if err == nil {
		t.Fatal("expected an error after cancellation")
	}
	if elapsed := stopped.Sub(<-cancelled); elapsed > time.Second {
		t.Fatalf("journalctl stopped %v after cancellation", elapsed)
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
