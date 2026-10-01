package domain

import "time"

// DatabaseHistory describes operations performed on this database. Unknown
// timestamps remain nil; filesystem times are not evidence of a restore.
type DatabaseHistory struct {
	LastBackupGeneratedAt *time.Time
	LastRestoredAt        *time.Time
	RestoredSnapshotAt    *time.Time
}
