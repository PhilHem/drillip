package cli

import (
	"context"
	"fmt"
	"io"
	"time"
)

func (c *CLI) runHealth(ctx context.Context, cmd *Command, w io.Writer) error {
	if c.Health == nil {
		return fmt.Errorf("health requires server access")
	}
	if !cmd.details {
		if err := c.Health.Health(ctx); err != nil {
			return fmt.Errorf("unhealthy: %w", err)
		}
		_, err := fmt.Fprintln(w, "ok")
		return err
	}
	history, err := c.Health.DatabaseHistory(ctx)
	if err != nil {
		return err
	}
	format := func(t *time.Time) string {
		if t == nil {
			return "unknown"
		}
		return t.UTC().Format(time.RFC3339Nano)
	}
	_, err = fmt.Fprintf(w, "status: ok\nlast_backup_generated_at: %s\nlast_restored_at: %s\nrestored_snapshot_at: %s\n",
		format(history.LastBackupGeneratedAt), format(history.LastRestoredAt), format(history.RestoredSnapshotAt))
	return err
}
