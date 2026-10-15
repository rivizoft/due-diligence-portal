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

	"github.com/rivizoft/due-diligence-portal/internal/domain"
	"github.com/rivizoft/due-diligence-portal/internal/repository"
	storesvc "github.com/rivizoft/due-diligence-portal/internal/service/storage"
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

func TestS3HTTPNDAGateAndSessionRevocation(t *testing.T) {
	for _, revoke := range []string{"deactivate", "password reset"} {
		t.Run(revoke, func(t *testing.T) {
			e, auth, admin, db, _, objects := setupStorageHTTP(t)
			setInvestorAccessMode(t, "granted")
			require.Equal(t, 200, storageHTTP(t, e, admin, "PUT", "/storage", storageHTTPConfig()).Code)
			id := uploadDocument(t, e, admin, "cat-financials")
			investor := loginInvestor(t, auth, admin)
			ic, err := auth.ValidateToken(investor)
			require.NoError(t, err)
			ac, err := auth.ValidateToken(admin)
			require.NoError(t, err)
			ctx := context.Background()
			nda := repository.NewNDARepository(db)
			require.NoError(t, nda.CreateTemplate(ctx, &domain.NDATemplate{ID: "s3-nda", Name: "NDA", Content: "Terms", IsActive: true, Version: 1, CreatedBy: ac.UserID}))
			require.NoError(t, repository.NewPermissionRepository(db).Grant(ctx, &domain.AccessGrant{ID: "s3-download", UserID: ic.UserID, ResourceType: domain.ResourceDocument, ResourceID: id, AccessLevel: domain.AccessDownload, GrantedBy: ac.UserID}))
			reads := objects.reads
			path := "/documents/" + id
			for _, suffix := range []string{"", "/download", "/versions/1", "/preview", "/preview-info", "/preview-pages/1", "/versions/1/preview", "/versions/1/preview-info", "/versions/1/preview-pages/1"} {
				require.Equal(t, 403, documentRequest(e, investor, "GET", path+suffix).Code, suffix)
			}
			require.Equal(t, reads, objects.reads, "NDA denial must occur before reading S3")
			require.NoError(t, nda.CreateSignature(ctx, &domain.NDASignature{ID: "s3-signed", UserID: ic.UserID, TemplateID: "s3-nda", SignerName: ic.Name, SignerEmail: ic.Email, IPAddress: "127.0.0.1"}))
			allowed := documentRequest(e, investor, "GET", path+"/download")
			require.Equal(t, 200, allowed.Code)
			require.Equal(t, "fake pdf content", allowed.Body.String())
			reads = objects.reads
			if revoke == "deactivate" {
				require.NoError(t, repository.NewUserRepository(db).Deactivate(ctx, ic.UserID))
			} else {
				_, err = auth.SetPassword(ctx, ic.UserID, "replacement123")
				require.NoError(t, err)
			}
			require.Equal(t, 401, documentRequest(e, investor, "GET", path+"/download").Code)
			require.Equal(t, reads, objects.reads, "a revoked session must not read S3")
		})
	}
}
