package domain

import "time"

// StorageConfig is server-only configuration. API handlers return a separate view.
type StorageConfig struct {
	Enabled           bool
	Bucket            string
	Region            string
	Endpoint          string
	PathStyle         bool
	KeyPrefix         string
	Credentials       []byte
	MaskedAccessKeyID string
	UpdatedAt         time.Time
}

// ObjectLocation identifies the private original for a version.
type ObjectLocation struct {
	Bucket string
	Key    string
}

// StorageCleanupJob survives logical version deletion and server restarts.
type StorageCleanupJob struct {
	ID       int64
	Location ObjectLocation
	Attempts int
}
