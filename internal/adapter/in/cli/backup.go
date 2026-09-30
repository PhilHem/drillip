package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func (c *CLI) runBackup(ctx context.Context, cmd *Command, w io.Writer) error {
	if c.Backups == nil {
		return fmt.Errorf("backup requires server access")
	}
	if _, err := os.Lstat(cmd.output); err == nil {
		return fmt.Errorf("backup output already exists: %s; choose a new file", cmd.output)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check backup output: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(cmd.output), ".drillip-backup-*")
	if err != nil {
		return fmt.Errorf("create backup output: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	backup, err := c.Backups.Backup(ctx)
	if err != nil {
		return err
	}
	defer backup.Close()
	written, err := io.Copy(file, backup)
	if err != nil {
		return fmt.Errorf("download database backup: %w", err)
	}
	if written != backup.Size() {
		return fmt.Errorf("incomplete database backup: received %d of %d bytes", written, backup.Size())
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("save database backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	// Linking a complete file publishes it atomically and refuses a destination
	// created during the download, unlike Rename on Unix.
	if err := os.Link(file.Name(), cmd.output); err != nil {
		return fmt.Errorf("save database backup without replacing an existing file: %w", err)
	}
	_, err = fmt.Fprintf(w, "saved %s\n", cmd.output)
	return err
}
