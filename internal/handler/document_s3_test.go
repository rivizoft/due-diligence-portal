package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/witfoo/due-diligence-portal/internal/domain"
	"github.com/witfoo/due-diligence-portal/internal/repository"
	storesvc "github.com/witfoo/due-diligence-portal/internal/service/storage"
)

func TestS3PDFViewOnlyAndWatermarked(t *testing.T) {
	d := setupDelivery(t, nil)
	m := &handlerObjectStore{objects: map[string][]byte{}}
	svc := storesvc.NewService(repository.NewStorageRepository(d.db), d.docs, filepath.Join(t.TempDir(), "storage.key"), func(_ *domain.StorageConfig, _ storesvc.Credentials) (storesvc.Client, error) { return m, nil })
	ctx := context.Background()
	_, err := svc.Update(ctx, storageHTTPConfig())
	require.NoError(t, err)
	doc, err := d.docs.GetByID(ctx, "pdf")
	require.NoError(t, err)
	v := &domain.DocumentVersion{ID: "s3-pdf", DocumentID: doc.ID, FileData: d.data, FileSize: int64(len(d.data)), MimeType: "application/pdf", ChecksumSHA256: fmt.Sprintf("%x", sha256.Sum256(d.data)), UploadedBy: "staff"}
	require.NoError(t, svc.Store(ctx, v, func() error { return d.docs.AddVersion(ctx, doc, v) }))
	d.handler.content = svc
	reads := m.reads
	require.Equal(t, 404, d.get("/download", "viewer").Code)
	require.Equal(t, reads, m.reads)
	download := d.get("/download", "staff")
	require.Equal(t, 200, download.Code, download.Body.String())
	require.NotEqual(t, d.data, download.Body.Bytes())
	require.Equal(t, "application/pdf", download.Header().Get("Content-Type"))
	info := d.get("/preview-info", "viewer")
	require.Equal(t, 200, info.Code, info.Body.String())
	require.Contains(t, info.Body.String(), `"mode":"images"`)
	require.Contains(t, info.Body.String(), `"version_number":2`)
	page := d.get("/preview-pages/1", "viewer")
	require.Equal(t, 200, page.Code, page.Body.String())
	require.Equal(t, "image/png", page.Header().Get("Content-Type"))
	require.NotContains(t, page.Body.String(), "%PDF")
	require.NoError(t, d.perms.Revoke(ctx, "grant"))
	require.Equal(t, 404, d.get("/preview-pages/1", "viewer").Code)
}

func TestS3UploadFailureDoesNotCreateMetadata(t *testing.T) {
	e, _, admin, db, _, m := setupStorageHTTP(t)
	require.Equal(t, 200, storageHTTP(t, e, admin, "PUT", "/storage", storageHTTPConfig()).Code)
	m.putError = context.DeadlineExceeded
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "test.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("data"))
	require.NoError(t, err)
	require.NoError(t, w.WriteField("name", "Test"))
	require.NoError(t, w.WriteField("category_id", "cat-financials"))
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+admin)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, 504, rec.Code, rec.Body.String())
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&count))
	require.Zero(t, count)
	require.Empty(t, m.objects)
}
