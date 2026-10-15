package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
	"github.com/rivizoft/due-diligence-portal/internal/repository"
	storesvc "github.com/rivizoft/due-diligence-portal/internal/service/storage"
)

func uploadDeliveryPDF(t *testing.T, d *deliveryTest, path string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	p, err := w.CreateFormFile("file", "report.pdf") // multipart default is application/octet-stream
	require.NoError(t, err)
	_, err = p.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.WriteField("name", "Report.pdf"))
	require.NoError(t, w.WriteField("category_id", "cat-financials"))
	require.NoError(t, w.Close())
	req := httptest.NewRequest("POST", "/api/v1"+path, &buf)
	req.Header.Set("Authorization", "staff")
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	d.e.ServeHTTP(rec, req)
	return rec
}

func TestPDFWithLeadingBytesUploadAndDelivery(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "sqlite", true: "s3"}[remote], func(t *testing.T) {
			d := setupDelivery(t, nil)
			if remote {
				m := &handlerObjectStore{objects: map[string][]byte{}}
				svc := storesvc.NewService(repository.NewStorageRepository(d.db), d.docs, filepath.Join(t.TempDir(), "storage.key"), func(_ *domain.StorageConfig, _ storesvc.Credentials) (storesvc.Client, error) { return m, nil })
				_, err := svc.Update(context.Background(), storageHTTPConfig())
				require.NoError(t, err)
				d.handler.content = svc
			}
			data := append([]byte{0, 0xef, 0xbb, 0xbf, '\n'}, d.data...)
			upload := uploadDeliveryPDF(t, d, "/documents", data)
			require.Equal(t, 201, upload.Code, upload.Body.String())
			var result struct{ Data domain.Document }
			require.NoError(t, json.Unmarshal(upload.Body.Bytes(), &result))
			require.Equal(t, "application/pdf", result.Data.MimeType)
			original, err := d.handler.readVersion(context.Background(), result.Data.ID, 1)
			require.NoError(t, err)
			require.Equal(t, data, original, "MIME detection must preserve original bytes")
			version := uploadDeliveryPDF(t, d, "/documents/pdf/versions", data)
			require.Equal(t, 201, version.Code, version.Body.String())
			var vr struct{ Data domain.DocumentVersion }
			require.NoError(t, json.Unmarshal(version.Body.Bytes(), &vr))
			require.Equal(t, "application/pdf", vr.Data.MimeType)
			down := d.get("/download", "staff")
			require.Equal(t, 200, down.Code, down.Body.String())
			require.Equal(t, "application/pdf", down.Header().Get("Content-Type"))
			require.NotEqual(t, data, down.Body.Bytes(), "PDF download must pass through watermarking")
			page := d.get("/preview-pages/1", "viewer")
			require.Equal(t, 200, page.Code, page.Body.String())
			require.Equal(t, "image/png", page.Header().Get("Content-Type"))
		})
	}
}

func TestLegacyGenericPDFMIME(t *testing.T) {
	for _, route := range []string{"", "/download", "/preview", "/preview-info", "/preview-pages/1"} {
		t.Run(route, func(t *testing.T) {
			d := setupDelivery(t, nil)
			m := &handlerObjectStore{objects: map[string][]byte{}}
			svc := storesvc.NewService(repository.NewStorageRepository(d.db), d.docs, filepath.Join(t.TempDir(), "storage.key"), func(_ *domain.StorageConfig, _ storesvc.Credentials) (storesvc.Client, error) { return m, nil })
			ctx := context.Background()
			_, err := svc.Update(ctx, storageHTTPConfig())
			require.NoError(t, err)
			d.handler.content = svc
			// A second, S3-backed version alongside the existing SQLite original.
			upload := uploadDeliveryPDF(t, d, "/documents/pdf/versions", d.data)
			require.Equal(t, 201, upload.Code, upload.Body.String())
			_, err = d.db.Exec(`UPDATE documents SET mime_type='application/octet-stream'; UPDATE document_versions SET mime_type='application/octet-stream'`)
			require.NoError(t, err)
			reads := m.reads
			prefixReads := m.prefixReads
			require.NoError(t, d.perms.Revoke(ctx, "grant"))
			require.Equal(t, 404, d.get(route, "viewer").Code)
			require.Equal(t, reads, m.reads, "MIME repair must run after permission checks")
			require.Equal(t, prefixReads, m.prefixReads, "denied users must not sniff S3 content")
			r := d.get(route, "staff")
			require.Equal(t, 200, r.Code, r.Body.String())
			if route == "" {
				require.Contains(t, r.Body.String(), `"mime_type":"application/pdf"`)
				require.NotContains(t, r.Body.String(), "application/octet-stream", "historical PDFs also need their View action restored")
			}
			if route == "/download" || route == "/preview" {
				require.Equal(t, "application/pdf", r.Header().Get("Content-Type"))
				require.NotEqual(t, d.data, r.Body.Bytes())
			}
			doc, err := d.docs.GetByID(context.Background(), "pdf")
			require.NoError(t, err)
			require.Equal(t, "application/pdf", doc.MimeType)
		})
	}
}

func TestDocumentDetailsUseBoundedReadsAndTolerateUnavailableContent(t *testing.T) {
	d := setupDelivery(t, nil)
	m := &handlerObjectStore{objects: map[string][]byte{}}
	svc := storesvc.NewService(repository.NewStorageRepository(d.db), d.docs, filepath.Join(t.TempDir(), "storage.key"), func(_ *domain.StorageConfig, _ storesvc.Credentials) (storesvc.Client, error) { return m, nil })
	ctx := context.Background()
	_, err := svc.Update(ctx, storageHTTPConfig())
	require.NoError(t, err)
	d.handler.content = svc
	upload := uploadDeliveryPDF(t, d, "/documents/pdf/versions", d.data)
	require.Equal(t, 201, upload.Code, upload.Body.String())
	_, err = d.db.Exec(`UPDATE documents SET mime_type='application/octet-stream'; UPDATE document_versions SET mime_type='application/octet-stream'`)
	require.NoError(t, err)
	before := m.reads
	res := d.get("", "staff")
	require.Equal(t, 200, res.Code, res.Body.String())
	require.Equal(t, before, m.reads, "metadata repair must not fetch whole S3 objects")
	require.Equal(t, 1, m.prefixReads, "the S3 original only needs a prefix read")
	// An unavailable current original must not prevent viewing/editing metadata.
	clear(m.objects)
	_, err = d.db.Exec(`UPDATE documents SET mime_type='application/octet-stream'; UPDATE document_versions SET mime_type='application/octet-stream'`)
	require.NoError(t, err)
	require.Equal(t, 200, d.get("", "staff").Code)
	require.NotEqual(t, 200, d.get("/download", "staff").Code, "delivery must still fail closed")
	// A broken historical S3 object must not block a newer SQLite version.
	config := storageHTTPConfig()
	config.Enabled = false
	_, err = svc.Update(ctx, config)
	require.NoError(t, err)
	upload = uploadDeliveryPDF(t, d, "/documents/pdf/versions", d.data)
	require.Equal(t, 201, upload.Code, upload.Body.String())
	prefixReads := m.prefixReads
	require.Equal(t, 200, d.get("", "staff").Code)
	require.Equal(t, prefixReads+1, m.prefixReads, "historical read failure must not block metadata")
}

func TestS3DeleteDocumentQueuesEveryVersionAndRetries(t *testing.T) {
	e, _, admin, db, svc, objects := setupStorageHTTP(t)
	// Include a local version before enabling S3.
	id := uploadDocument(t, e, admin, "cat-financials")
	require.Equal(t, 200, storageHTTP(t, e, admin, "PUT", "/storage", storageHTTPConfig()).Code)
	uploadTestVersion(t, e, admin, id, "remote second")
	uploadTestVersion(t, e, admin, id, "remote third")
	require.Len(t, objects.objects, 2)
	rec := documentRequest(e, admin, "DELETE", "/documents/"+id)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	var jobs, versions, refs int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM storage_cleanup_jobs`).Scan(&jobs))
	require.Equal(t, 2, jobs)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM document_versions WHERE document_id=?`, id).Scan(&versions))
	require.Zero(t, versions)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM document_version_storage`).Scan(&refs))
	require.Zero(t, refs)
	require.Equal(t, 404, documentRequest(e, admin, "GET", "/documents/"+id+"/download").Code)
	require.Equal(t, 200, documentRequest(e, admin, "DELETE", "/documents/"+id).Code)
	objects.deleteError = errors.New("offline")
	svc.Cleanup(context.Background())
	require.Len(t, objects.objects, 2)
	objects.deleteError = nil
	_, err := db.Exec(`UPDATE storage_cleanup_jobs SET next_attempt=0`)
	require.NoError(t, err)
	svc.Cleanup(context.Background())
	require.Empty(t, objects.objects)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM storage_cleanup_jobs`).Scan(&jobs))
	require.Zero(t, jobs)
	settings, err := svc.Settings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.LocationLocked)
}
