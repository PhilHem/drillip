package httpwire

import "time"

const FeatureDatabaseHistory = "database_history"

// HealthDetails retains null values for unknown timestamps.
type HealthDetails struct {
	Status                string     `json:"status"`
	LastBackupGeneratedAt *time.Time `json:"last_backup_generated_at"`
	LastRestoredAt        *time.Time `json:"last_restored_at"`
	RestoredSnapshotAt    *time.Time `json:"restored_snapshot_at"`
}
