package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
)

func TestStorageConfigAndIdempotentMigration(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	r := NewStorageRepository(db)
	c, err := r.GetConfig(ctx)
	require.NoError(t, err)
	require.False(t, c.Enabled)
	c.Enabled = true
	c.Bucket = "private-bucket"
	c.Region = "eu-central-1"
	c.Credentials = []byte("ciphertext")
	c.MaskedAccessKeyID = "****test"
	require.NoError(t, r.SaveConfig(ctx, c))
	require.NoError(t, db.Migrate())
	got, err := r.GetConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, c.Bucket, got.Bucket)
	require.Equal(t, c.Credentials, got.Credentials)
	locked, err := r.HasReferences(ctx)
	require.NoError(t, err)
	require.False(t, locked)
}

func TestS3VersionReferencesAndDeletionOutbox(t *testing.T) {
	db := setupTestDB(t)
	user, cat := seedDocumentDeps(t, db)
	ctx := context.Background()
	docs := NewDocumentRepository(db)
	sr := NewStorageRepository(db)
	doc := &domain.Document{ID: "s3-doc", Name: "Test", CategoryID: cat, UploadedBy: user, CurrentVersion: 1, MimeType: "text/plain", FileSize: 4}
	v := &domain.DocumentVersion{ID: "s3-v1", DocumentID: doc.ID, VersionNumber: 1, FileData: []byte{}, FileSize: 4, MimeType: "text/plain", ChecksumSHA256: "test", UploadedBy: user, Storage: &domain.ObjectLocation{Bucket: "private", Key: "docs/v1"}}
	require.NoError(t, docs.CreateWithVersion(ctx, doc, v))
	got, err := docs.GetVersion(ctx, doc.ID, 1)
	require.NoError(t, err)
	require.Equal(t, v.Storage, got.Storage)
	require.Empty(t, got.FileData)
	meta, err := docs.GetVersionMetadata(ctx, doc.ID, 1)
	require.NoError(t, err)
	require.Equal(t, v.Storage, meta.Storage)
	require.ErrorIs(t, docs.DeleteVersion(ctx, doc.ID, 1), domain.ErrLastVersion)
	jobs, err := sr.DueCleanup(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, jobs)
	v2 := &domain.DocumentVersion{ID: "sqlite-v2", DocumentID: doc.ID, FileData: []byte("local"), FileSize: 5, MimeType: "text/plain", ChecksumSHA256: "test", UploadedBy: user}
	require.NoError(t, docs.AddVersion(ctx, doc, v2))
	require.NoError(t, docs.DeleteVersion(ctx, doc.ID, 1))
	jobs, err = sr.DueCleanup(ctx, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, *v.Storage, jobs[0].Location)
	locked, err := sr.HasReferences(ctx)
	require.NoError(t, err)
	require.True(t, locked)
	require.NoError(t, sr.RetryCleanup(ctx, jobs[0]))
	due, err := sr.DueCleanup(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, due)
	require.NoError(t, sr.FinishCleanup(ctx, jobs[0].ID))
	locked, err = sr.HasReferences(ctx)
	require.NoError(t, err)
	require.False(t, locked)
	got, err = docs.GetVersion(ctx, doc.ID, 2)
	require.NoError(t, err)
	require.Nil(t, got.Storage)
	require.Equal(t, []byte("local"), got.FileData)
}

func TestS3ReferenceRollback(t *testing.T) {
	db := setupTestDB(t)
	user, cat := seedDocumentDeps(t, db)
	ctx := context.Background()
	docs := NewDocumentRepository(db)
	doc := &domain.Document{ID: "rollback", Name: "Test", CategoryID: cat, UploadedBy: user, CurrentVersion: 1, MimeType: "text/plain", FileSize: 1}
	v := &domain.DocumentVersion{ID: "bad", DocumentID: doc.ID, VersionNumber: 1, FileData: []byte{}, FileSize: 1, MimeType: "text/plain", UploadedBy: user, Storage: &domain.ObjectLocation{Bucket: "", Key: "key"}}
	require.Error(t, docs.CreateWithVersion(ctx, doc, v))
	_, err := docs.GetByID(ctx, doc.ID)
	require.ErrorIs(t, err, domain.ErrDocumentNotFound)
}

func TestDocumentArchiveCleanupIsAtomic(t *testing.T) {
	db := setupTestDB(t)
	user, cat := seedDocumentDeps(t, db)
	ctx := context.Background()
	docs := NewDocumentRepository(db)
	doc := &domain.Document{ID: "archive", Name: "Test", CategoryID: cat, UploadedBy: user, CurrentVersion: 1}
	v := &domain.DocumentVersion{ID: "archive-v1", DocumentID: doc.ID, VersionNumber: 1, FileData: []byte{}, FileSize: 4, MimeType: "application/pdf", UploadedBy: user, Storage: &domain.ObjectLocation{Bucket: "private", Key: "docs/archive-v1"}}
	require.NoError(t, docs.CreateWithVersion(ctx, doc, v))
	_, err := db.Exec(`CREATE TRIGGER fail_cleanup BEFORE INSERT ON storage_cleanup_jobs BEGIN SELECT RAISE(ABORT, 'disk error'); END`)
	require.NoError(t, err)
	require.Error(t, docs.Archive(ctx, doc.ID))
	got, err := docs.GetByID(ctx, doc.ID)
	require.NoError(t, err)
	require.False(t, got.IsArchived)
	version, err := docs.GetVersion(ctx, doc.ID, 1)
	require.NoError(t, err)
	require.Equal(t, v.Storage, version.Storage)
	_, err = db.Exec(`DROP TRIGGER fail_cleanup`)
	require.NoError(t, err)
	require.NoError(t, docs.Archive(ctx, doc.ID))
	_, err = docs.GetVersion(ctx, doc.ID, 1)
	require.ErrorIs(t, err, domain.ErrVersionNotFound)
	jobs, err := NewStorageRepository(db).DueCleanup(ctx, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, *v.Storage, jobs[0].Location)
	// A stale uploader must not recreate content after deletion committed.
	next := &domain.DocumentVersion{ID: "stale-upload", DocumentID: doc.ID, FileData: []byte("late"), UploadedBy: user}
	require.ErrorIs(t, docs.AddVersion(ctx, doc, next), domain.ErrDocumentNotFound)
}
