package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
	"github.com/rivizoft/due-diligence-portal/internal/repository"
)

type objectMemory struct {
	mu                           sync.Mutex
	objects                      map[string][]byte
	failPut, failGet, failDelete bool
	reads                        int
	writes                       int
	block                        chan struct{}
	entered                      chan struct{}
	deleteDeadline               bool
	deleteGate                   chan struct{}
	deleteEntered                chan struct{}
}

func TestStorageConfigChangeWaitsForUpload(t *testing.T) {
	svc, _, m, docs, _ := storageFixture(t)
	ctx := context.Background()
	_, err := svc.Update(ctx, validSave())
	require.NoError(t, err)
	m.entered = make(chan struct{})
	m.block = make(chan struct{})
	doc := docForTest()
	v := versionForTest("concurrent", []byte("data"))
	stored := make(chan error, 1)
	go func() { stored <- svc.Store(ctx, v, func() error { return docs.CreateWithVersion(ctx, doc, v) }) }()
	<-m.entered
	req := validSave()
	req.Enabled = false
	req.AccessKeyID = ""
	req.SecretAccessKey = ""
	req.Bucket = "other-bucket"
	updated := make(chan error, 1)
	go func() { _, e := svc.Update(ctx, req); updated <- e }()
	select {
	case e := <-updated:
		t.Fatalf("configuration changed while upload active: %v", e)
	case <-time.After(20 * time.Millisecond):
	}
	close(m.block)
	require.NoError(t, <-stored)
	require.ErrorIs(t, <-updated, ErrLocationLocked)
}

func TestStorageReadAndProbeFailuresAreSafe(t *testing.T) {
	svc, _, m, docs, _ := storageFixture(t)
	ctx := context.Background()
	_, err := svc.Update(ctx, validSave())
	require.NoError(t, err)
	m.failGet = true
	req := validSave()
	req.AccessKeyID = ""
	req.SecretAccessKey = ""
	err = svc.Test(ctx, req)
	require.ErrorIs(t, err, ErrUnavailable)
	require.NotContains(t, err.Error(), "private upstream detail")
	require.Empty(t, m.objects)
	m.failGet = false
	doc := docForTest()
	v := versionForTest("read", []byte("original"))
	require.NoError(t, svc.Store(ctx, v, func() error { return docs.CreateWithVersion(ctx, doc, v) }))
	m.failGet = true
	_, err = svc.Read(ctx, doc.ID, 1)
	require.ErrorIs(t, err, ErrUnavailable)
	m.failGet = false
	m.failDelete = true
	require.ErrorIs(t, svc.Test(ctx, req), ErrUnavailable)
}

func (m *objectMemory) Put(ctx context.Context, b, k string, data []byte, _ string) error {
	if m.entered != nil {
		close(m.entered)
		select {
		case <-m.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failPut {
		return errors.New("private upstream detail")
	}
	m.objects[b+"/"+k] = append([]byte{}, data...)
	m.writes++
	return nil
}
func (m *objectMemory) Get(_ context.Context, b, k string, maxBytes int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	if m.failGet {
		return nil, errors.New("private upstream detail")
	}
	data, ok := m.objects[b+"/"+k]
	if !ok {
		return nil, ErrContent
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrContent
	}
	return append([]byte{}, data...), nil
}
func (m *objectMemory) Delete(ctx context.Context, b, k string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteDeadline {
		<-ctx.Done()
		return ctx.Err()
	}
	if m.deleteGate != nil {
		close(m.deleteEntered)
		select {
		case <-m.deleteGate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if m.failDelete {
		return errors.New("denied")
	}
	delete(m.objects, b+"/"+k)
	return nil
}

func TestCompensatingDeleteTimeoutStillQueuesRetry(t *testing.T) {
	svc, db, m, _, _ := storageFixture(t)
	ctx := context.Background()
	_, err := svc.Update(ctx, validSave())
	require.NoError(t, err)
	m.deleteDeadline = true
	v := versionForTest("timeout-orphan", []byte("data"))
	require.Error(t, svc.Store(ctx, v, func() error { return errors.New("sql failed") }))
	var jobs int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM storage_cleanup_jobs`).Scan(&jobs))
	require.Equal(t, 1, jobs, "a timed-out Delete must still leave a durable retry")
}

func TestStalledCleanupDoesNotBlockOfflineDisableOrSQLiteRead(t *testing.T) {
	svc, db, m, docs, _ := storageFixture(t)
	ctx := context.Background()
	doc := docForTest()
	v := versionForTest("local-before-s3", []byte("local"))
	require.NoError(t, svc.Store(ctx, v, func() error { return docs.CreateWithVersion(ctx, doc, v) }))
	_, err := svc.Update(ctx, validSave())
	require.NoError(t, err)
	require.NoError(t, repository.NewStorageRepository(db).QueueCleanup(ctx, domain.ObjectLocation{Bucket: "private-bucket", Key: "old-object"}))
	m.deleteGate = make(chan struct{})
	m.deleteEntered = make(chan struct{})
	cleanupDone := make(chan struct{})
	go func() { svc.Cleanup(ctx); close(cleanupDone) }()
	<-m.deleteEntered
	req := validSave()
	req.Enabled = false
	req.AccessKeyID = ""
	req.SecretAccessKey = ""
	updateDone := make(chan error, 1)
	go func() { _, e := svc.Update(ctx, req); updateDone <- e }()
	select {
	case e := <-updateDone:
		require.NoError(t, e)
	case <-time.After(200 * time.Millisecond):
		close(m.deleteGate)
		<-cleanupDone
		t.Fatal("offline disable blocked behind stalled cleanup")
	}
	data, err := svc.Read(ctx, doc.ID, 1)
	require.NoError(t, err)
	require.Equal(t, "local", string(data))
	close(m.deleteGate)
	<-cleanupDone
}

func TestCleanupSurvivesDatabaseReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portal.db")
	keyPath := filepath.Join(dir, "storage.key")
	ctx := context.Background()
	db, err := repository.New(path)
	require.NoError(t, err)
	require.NoError(t, db.Migrate())
	encrypted, err := newCredentialCipher(keyPath).Seal(Credentials{AccessKeyID: "access", SecretAccessKey: "secret"})
	require.NoError(t, err)
	r := repository.NewStorageRepository(db)
	require.NoError(t, r.SaveConfig(ctx, &domain.StorageConfig{Bucket: "private-bucket", Region: "eu-central-1", Credentials: encrypted}))
	require.NoError(t, r.QueueCleanup(ctx, domain.ObjectLocation{Bucket: "private-bucket", Key: "old-object"}))
	require.NoError(t, db.Close())
	reopened, err := repository.New(path)
	require.NoError(t, err)
	defer reopened.Close()
	require.NoError(t, reopened.Migrate())
	m := &objectMemory{objects: map[string][]byte{"private-bucket/old-object": []byte("old")}}
	r = repository.NewStorageRepository(reopened)
	svc := NewService(r, repository.NewDocumentRepository(reopened), keyPath, func(_ *domain.StorageConfig, _ Credentials) (Client, error) { return m, nil })
	svc.Cleanup(ctx)
	jobs, err := r.DueCleanup(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, jobs)
	require.Empty(t, m.objects)
}

func TestCleanupWorkerWakesForNewDeletion(t *testing.T) {
	svc, db, m, _, _ := storageFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := svc.Update(ctx, validSave())
	require.NoError(t, err)
	repo := repository.NewStorageRepository(db)
	require.NoError(t, repo.QueueCleanup(ctx, domain.ObjectLocation{Bucket: "private-bucket", Key: "startup-job"}))
	done := make(chan struct{})
	go func() { svc.RunCleanup(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	countJobs := func() int {
		var count int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM storage_cleanup_jobs`).Scan(&count))
		return count
	}
	require.Eventually(t, func() bool { return countJobs() == 0 }, 2*time.Second, 10*time.Millisecond)
	m.mu.Lock()
	m.objects["private-bucket/new-job"] = []byte("old")
	m.mu.Unlock()
	require.NoError(t, repo.QueueCleanup(ctx, domain.ObjectLocation{Bucket: "private-bucket", Key: "new-job"}))
	svc.ScheduleCleanup()
	require.Eventually(t, func() bool { return countJobs() == 0 }, 2*time.Second, 10*time.Millisecond)
	m.mu.Lock()
	require.Empty(t, m.objects)
	m.mu.Unlock()
}
func storageFixture(t *testing.T) (*Service, *repository.DB, *objectMemory, repository.DocumentRepository, string) {
	t.Helper()
	db, err := repository.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Migrate())
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash,role,is_active) VALUES('storage-user','storage@test','Storage','hash','admin',1)`)
	require.NoError(t, err)
	m := &objectMemory{objects: map[string][]byte{}}
	docs := repository.NewDocumentRepository(db)
	path := filepath.Join(t.TempDir(), "storage.key")
	svc := NewService(repository.NewStorageRepository(db), docs, path, func(_ *domain.StorageConfig, _ Credentials) (Client, error) { return m, nil })
	return svc, db, m, docs, path
}
func validSave() SaveRequest {
	return SaveRequest{Enabled: true, Bucket: "private-bucket", Region: "eu-central-1", AccessKeyID: "test-access", SecretAccessKey: "test-secret", KeyPrefix: "docs"}
}
func versionForTest(id string, data []byte) *domain.DocumentVersion {
	sum := sha256.Sum256(data)
	return &domain.DocumentVersion{ID: id, DocumentID: "storage-doc", VersionNumber: 1, FileData: data, FileSize: int64(len(data)), MimeType: "text/plain", ChecksumSHA256: hex.EncodeToString(sum[:]), UploadedBy: "storage-user"}
}
func docForTest() *domain.Document {
	return &domain.Document{ID: "storage-doc", Name: "Storage", CategoryID: "cat-financials", UploadedBy: "storage-user", CurrentVersion: 1, MimeType: "text/plain"}
}

func TestStorageProbeAndSecretPreservation(t *testing.T) {
	svc, db, m, _, _ := storageFixture(t)
	ctx := context.Background()
	req := validSave()
	require.NoError(t, svc.Test(ctx, req))
	cfg, err := svc.Settings(ctx)
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	require.Empty(t, m.objects)
	cfg, err = svc.Update(ctx, req)
	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	require.Equal(t, "docs/", cfg.KeyPrefix)
	require.True(t, cfg.CredentialsConfigured)
	require.NotContains(t, cfg.MaskedAccessKeyID, "test-access")
	var encrypted []byte
	require.NoError(t, db.QueryRow(`SELECT credentials FROM storage_config`).Scan(&encrypted))
	require.NotContains(t, string(encrypted), "test-secret")
	req.AccessKeyID = ""
	req.SecretAccessKey = ""
	cfg, err = svc.Update(ctx, req)
	require.NoError(t, err)
	require.True(t, cfg.CredentialsConfigured)
	req.SecretAccessKey = "half"
	_, err = svc.Update(ctx, req)
	require.ErrorAs(t, err, new(*ValidationError))
}

func TestShortAccessKeyIsFullyMasked(t *testing.T) {
	svc, _, _, _, _ := storageFixture(t)
	req := validSave()
	req.AccessKeyID = "abc"
	settings, err := svc.Update(context.Background(), req)
	require.NoError(t, err)
	require.NotContains(t, settings.MaskedAccessKeyID, "abc")
}
func TestStorageValidationAndFailedEnable(t *testing.T) {
	for _, mutate := range []func(*SaveRequest){func(r *SaveRequest) { r.Endpoint = "http://example.com" }, func(r *SaveRequest) { r.Endpoint = "https://u:p@example.com" }, func(r *SaveRequest) { r.KeyPrefix = "../bad" }, func(r *SaveRequest) { r.Bucket = "bad/bucket" }, func(r *SaveRequest) { r.Region = "" }, func(r *SaveRequest) { r.AccessKeyID = "" }} {
		svc, _, _, _, _ := storageFixture(t)
		r := validSave()
		mutate(&r)
		_, err := svc.Update(context.Background(), r)
		require.ErrorAs(t, err, new(*ValidationError))
	}
	svc, _, m, _, _ := storageFixture(t)
	m.failPut = true
	_, err := svc.Update(context.Background(), validSave())
	require.ErrorIs(t, err, ErrUnavailable)
	cfg, err := svc.Settings(context.Background())
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
}
func TestStorageMixedVersionsOfflineDisableAndCleanup(t *testing.T) {
	svc, db, m, docs, _ := storageFixture(t)
	ctx := context.Background()
	doc := docForTest()
	local := versionForTest("local", []byte("local"))
	require.NoError(t, svc.Store(ctx, local, func() error { return docs.CreateWithVersion(ctx, doc, local) }))
	_, err := svc.Update(ctx, validSave())
	require.NoError(t, err)
	remote := versionForTest("remote", []byte("remote"))
	require.NoError(t, svc.Store(ctx, remote, func() error { return docs.AddVersion(ctx, doc, remote) }))
	got, err := docs.GetVersion(ctx, doc.ID, 2)
	require.NoError(t, err)
	require.Empty(t, got.FileData)
	require.Equal(t, "docs/storage-doc/remote", got.Storage.Key)
	req := validSave()
	req.Enabled = false
	req.AccessKeyID = ""
	req.SecretAccessKey = ""
	m.failPut = true
	_, err = svc.Update(ctx, req)
	require.NoError(t, err)
	data, err := svc.Read(ctx, doc.ID, 1)
	require.NoError(t, err)
	require.Equal(t, "local", string(data))
	data, err = svc.Read(ctx, doc.ID, 2)
	require.NoError(t, err)
	require.Equal(t, "remote", string(data))
	req.Bucket = "other-bucket"
	_, err = svc.Update(ctx, req)
	require.ErrorIs(t, err, ErrLocationLocked)
	m.failDelete = true
	require.NoError(t, docs.DeleteVersion(ctx, doc.ID, 2))
	svc.Cleanup(ctx)
	var jobs int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM storage_cleanup_jobs`).Scan(&jobs))
	require.Equal(t, 1, jobs)
	m.failDelete = false
	_, err = db.Exec(`UPDATE storage_cleanup_jobs SET next_attempt=0`)
	require.NoError(t, err)
	svc.Cleanup(ctx)
	require.Empty(t, m.objects)
}

func TestStorageIntegrityAndSQLCompensation(t *testing.T) {
	svc, db, m, docs, _ := storageFixture(t)
	ctx := context.Background()
	_, err := svc.Update(ctx, validSave())
	require.NoError(t, err)
	v := versionForTest("failed", []byte("data"))
	require.Error(t, svc.Store(ctx, v, func() error { return errors.New("sql failed") }))
	require.Empty(t, m.objects)
	m.failDelete = true
	v = versionForTest("retry", []byte("data"))
	require.Error(t, svc.Store(ctx, v, func() error { return errors.New("sql failed") }))
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM storage_cleanup_jobs`).Scan(&count))
	require.Equal(t, 1, count)
	m.failDelete = false
	svc.Cleanup(ctx)
	require.Empty(t, m.objects)
	doc := docForTest()
	v = versionForTest("ok", []byte("original"))
	require.NoError(t, svc.Store(ctx, v, func() error { return docs.CreateWithVersion(ctx, doc, v) }))
	m.objects[v.Storage.Bucket+"/"+v.Storage.Key] = []byte("tampered")
	_, err = svc.Read(ctx, doc.ID, 1)
	require.ErrorIs(t, err, ErrContent)
}
func TestMissingKeyDoesNotBreakSQLiteRead(t *testing.T) {
	svc, _, _, docs, path := storageFixture(t)
	ctx := context.Background()
	doc := docForTest()
	v := versionForTest("local", []byte("local"))
	require.NoError(t, svc.Store(ctx, v, func() error { return docs.CreateWithVersion(ctx, doc, v) }))
	_, err := svc.Update(ctx, validSave())
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))
	data, err := svc.Read(ctx, doc.ID, 1)
	require.NoError(t, err)
	require.Equal(t, "local", string(data))
	req := validSave()
	req.AccessKeyID = ""
	req.SecretAccessKey = ""
	require.Error(t, svc.Test(ctx, req))
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
}
