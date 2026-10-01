package domain

import (
	"errors"
	"io"
)

var ErrBackupBusy = errors.New("another database backup is in progress; try again after it finishes")

// DatabaseBackup is a complete database snapshot. Close releases its resources.
// Size is the exact number of bytes available from Read.
type DatabaseBackup interface {
	io.ReadCloser
	Size() int64
}
