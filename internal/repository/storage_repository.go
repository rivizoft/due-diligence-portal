package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
)

// StorageRepository persists encrypted settings and the object-deletion outbox.
type StorageRepository interface {
	GetConfig(context.Context) (*domain.StorageConfig, error)
	SaveConfig(context.Context, *domain.StorageConfig) error
	HasReferences(context.Context) (bool, error)
	QueueCleanup(context.Context, domain.ObjectLocation) error
	DueCleanup(context.Context, int) ([]*domain.StorageCleanupJob, error)
	FinishCleanup(context.Context, int64) error
	RetryCleanup(context.Context, *domain.StorageCleanupJob) error
}
type storageRepository struct{ db *DB }

// NewStorageRepository creates a SQLite-backed storage configuration repository.
func NewStorageRepository(db *DB) StorageRepository { return &storageRepository{db: db} }
func (r *storageRepository) GetConfig(ctx context.Context) (*domain.StorageConfig, error) {
	c := &domain.StorageConfig{}
	var updated string
	err := r.db.QueryRowContext(ctx, `SELECT enabled,bucket,region,endpoint,path_style,key_prefix,credentials,masked_access_key_id,updated_at FROM storage_config WHERE id='default'`).Scan(&c.Enabled, &c.Bucket, &c.Region, &c.Endpoint, &c.PathStyle, &c.KeyPrefix, &c.Credentials, &c.MaskedAccessKeyID, &updated)
	if err == sql.ErrNoRows {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get storage config: %w", err)
	}
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return c, nil
}
func (r *storageRepository) SaveConfig(ctx context.Context, c *domain.StorageConfig) error {
	c.UpdatedAt = time.Now().UTC()
	creds := c.Credentials
	if creds == nil {
		creds = []byte{}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO storage_config(id,enabled,bucket,region,endpoint,path_style,key_prefix,credentials,masked_access_key_id,updated_at) VALUES('default',?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled,bucket=excluded.bucket,region=excluded.region,endpoint=excluded.endpoint,path_style=excluded.path_style,key_prefix=excluded.key_prefix,credentials=excluded.credentials,masked_access_key_id=excluded.masked_access_key_id,updated_at=excluded.updated_at`, c.Enabled, c.Bucket, c.Region, c.Endpoint, c.PathStyle, c.KeyPrefix, creds, c.MaskedAccessKeyID, c.UpdatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("save storage config: %w", err)
	}
	return nil
}
func (r *storageRepository) HasReferences(ctx context.Context) (bool, error) {
	var yes bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM document_version_storage) OR EXISTS(SELECT 1 FROM storage_cleanup_jobs)`).Scan(&yes)
	return yes, err
}

const queueStorageCleanupSQL = `INSERT OR IGNORE INTO storage_cleanup_jobs(bucket,object_key) VALUES(?,?)`

func (r *storageRepository) QueueCleanup(ctx context.Context, l domain.ObjectLocation) error {
	_, err := r.db.ExecContext(ctx, queueStorageCleanupSQL, l.Bucket, l.Key)
	return err
}
func (r *storageRepository) DueCleanup(ctx context.Context, limit int) ([]*domain.StorageCleanupJob, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,bucket,object_key,attempts FROM storage_cleanup_jobs WHERE next_attempt<=? ORDER BY id LIMIT ?`, time.Now().Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []*domain.StorageCleanupJob
	for rows.Next() {
		j := &domain.StorageCleanupJob{}
		if err = rows.Scan(&j.ID, &j.Location.Bucket, &j.Location.Key, &j.Attempts); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}
func (r *storageRepository) FinishCleanup(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM storage_cleanup_jobs WHERE id=?`, id)
	return err
}
func (r *storageRepository) RetryCleanup(ctx context.Context, j *domain.StorageCleanupJob) error {
	attempts := min(j.Attempts+1, 12)
	delay := min(30*time.Second*time.Duration(1<<attempts), time.Hour)
	_, err := r.db.ExecContext(ctx, `UPDATE storage_cleanup_jobs SET attempts=attempts+1,next_attempt=? WHERE id=?`, time.Now().Add(delay).Unix(), j.ID)
	return err
}
