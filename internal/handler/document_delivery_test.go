package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/witfoo/due-diligence-portal/internal/domain"
	"github.com/witfoo/due-diligence-portal/internal/middleware"
	"github.com/witfoo/due-diligence-portal/internal/repository"
	pdfsvc "github.com/witfoo/due-diligence-portal/internal/service/pdf"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

type deliveryTest struct {
	handler *DocumentHandler
	e       *echo.Echo
	docs    repository.DocumentRepository
	wm      repository.WatermarkRepository
	perms   repository.PermissionRepository
	db      *repository.DB
	data    []byte
}

func setupDelivery(t *testing.T, engine pdfsvc.Watermarker) *deliveryTest {
	t.Helper()
	setInvestorAccessMode(t, "granted")
	db, e := repository.New(":memory:")
	require.NoError(t, e)
	require.NoError(t, db.Migrate())
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	for _, v := range []struct{ id, role string }{{"staff", domain.RoleAdmin}, {"viewer", domain.RoleInvestor}} {
		_, e = db.ExecContext(ctx, `INSERT INTO users(id,email,name,password_hash,role,is_active) VALUES(?,?,?,?,?,1)`, v.id, v.id+"@example.com", v.id, "hash", v.role)
		require.NoError(t, e)
	}
	cfg, e := pdfsvc.LoadConfig()
	require.NoError(t, e)
	cfg.CacheDir = t.TempDir()
	svc, e := pdfsvc.NewService(cfg, engine, pdfsvc.NewPoppler())
	require.NoError(t, e)
	t.Cleanup(func() { svc.Close() })
	docs := repository.NewDocumentRepository(db)
	wm := repository.NewWatermarkRepository(db)
	perms := repository.NewPermissionRepository(db)
	data, e := os.ReadFile("../service/pdf/testdata/sample.pdf")
	require.NoError(t, e)
	d := &domain.Document{ID: "pdf", Name: "Document.pdf", CategoryID: "cat-financials", UploadedBy: "staff", CurrentVersion: 1, MimeType: "application/pdf", FileSize: int64(len(data))}
	v := &domain.DocumentVersion{ID: "v1", DocumentID: d.ID, VersionNumber: 1, FileData: data, FileSize: int64(len(data)), MimeType: "application/pdf", ChecksumSHA256: fmt.Sprintf("%x", sha256.Sum256(data)), UploadedBy: "staff"}
	require.NoError(t, docs.CreateWithVersion(ctx, d, v))
	require.NoError(t, wm.UpsertConfig(ctx, &domain.WatermarkConfig{Enabled: true, TextTemplate: "PRIVATE {{user_email}}", Position: "center", Opacity: .4, FontSize: 18, Color: "#888888"}))
	require.NoError(t, perms.Grant(ctx, &domain.AccessGrant{ID: "grant", UserID: "viewer", ResourceType: domain.ResourceDocument, ResourceID: "pdf", AccessLevel: domain.AccessView, GrantedBy: "staff"}))
	app := echo.New()
	g := app.Group("/api/v1", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			id := c.Request().Header.Get("Authorization")
			if id != "staff" && id != "viewer" {
				return c.NoContent(401)
			}
			c.Set(middleware.ContextKeyUserID, id)
			c.Set(middleware.ContextKeyEmail, id+"@example.com")
			c.Set(middleware.ContextKeyName, id)
			role := domain.RoleInvestor
			if id == "staff" {
				role = domain.RoleAdmin
			}
			c.Set(middleware.ContextKeyRole, role)
			return next(c)
		}
	})
	h := NewDocumentHandler(docs, perms, middleware.NewAuditLogger(db), wm, svc)
	h.RegisterRoutes(g)
	return &deliveryTest{handler: h, e: app, docs: docs, wm: wm, perms: perms, db: db, data: data}
}
func (d *deliveryTest) get(path, actor string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/documents/pdf"+path, nil)
	r.Header.Set("Authorization", actor)
	rec := httptest.NewRecorder()
	d.e.ServeHTTP(rec, r)
	return rec
}
func TestDocumentDeliveryPermissions(t *testing.T) {
	d := setupDelivery(t, nil)
	for _, p := range []string{"/download", "/preview", "/versions/1", "/versions/1/preview"} {
		staff := d.get(p, "staff")
		require.Equal(t, 200, staff.Code, staff.Body.String())
		require.Equal(t, "application/pdf", staff.Header().Get("Content-Type"))
		require.NotEqual(t, d.data, staff.Body.Bytes())
		require.Equal(t, "private, no-store", staff.Header().Get("Cache-Control"))
		r := d.get(p, "viewer")
		require.Equal(t, 404, r.Code, r.Body.String())
		require.NotContains(t, r.Body.String(), "%PDF")
	}
	for _, p := range []string{"/preview-info", "/versions/1/preview-info"} {
		r := d.get(p, "viewer")
		require.Equal(t, 200, r.Code, r.Body.String())
		var res struct {
			Data struct {
				Mode    string            `json:"mode"`
				Version int               `json:"version_number"`
				Pages   []pdfsvc.PageInfo `json:"pages"`
			}
		}
		require.NoError(t, json.Unmarshal(r.Body.Bytes(), &res))
		require.Equal(t, "images", res.Data.Mode)
		require.Len(t, res.Data.Pages, 2)
		require.Equal(t, 1, res.Data.Version)
	}
	for _, p := range []string{"/preview-pages/1", "/versions/1/preview-pages/2"} {
		r := d.get(p, "viewer")
		require.Equal(t, 200, r.Code, r.Body.String())
		require.Equal(t, "image/png", r.Header().Get("Content-Type"))
		require.NotContains(t, r.Body.String(), "%PDF")
	}
	require.Equal(t, 401, d.get("/preview-info", "").Code)
	require.Equal(t, 400, d.get("/preview-pages/0", "viewer").Code)
	require.Equal(t, 422, d.get("/preview-pages/3", "viewer").Code)
	require.NoError(t, d.perms.Revoke(context.Background(), "grant"))
	require.Equal(t, 404, d.get("/preview-pages/1", "viewer").Code)
	require.NoError(t, d.docs.Archive(context.Background(), "pdf"))
	require.Equal(t, 404, d.get("/download", "staff").Code)
	require.Equal(t, 404, d.get("/preview-info", "staff").Code)
}
func TestDocumentDeliveryDisabledAndFailure(t *testing.T) {
	d := setupDelivery(t, nil)
	wc, e := d.wm.GetConfig(context.Background())
	require.NoError(t, e)
	wc.Enabled = false
	require.NoError(t, d.wm.UpsertConfig(context.Background(), wc))
	r := d.get("/download", "staff")
	require.Equal(t, 200, r.Code)
	require.Equal(t, d.data, r.Body.Bytes())
	wc.Enabled = true
	require.NoError(t, d.wm.UpsertConfig(context.Background(), wc))
	_, e = d.db.ExecContext(context.Background(), `UPDATE document_versions SET file_data=?, checksum_sha256=? WHERE id='v1'`, []byte("bad"), fmt.Sprintf("%x", sha256.Sum256([]byte("bad"))))
	require.NoError(t, e)
	r = d.get("/download", "staff")
	require.Equal(t, 422, r.Code, r.Body.String())
	require.NotEqual(t, "bad", r.Body.String())
}

type blockingWatermarker struct{ started, gate chan struct{} }

func (w blockingWatermarker) Apply(_ context.Context, in, out string, _ domain.WatermarkConfig, _ string) error {
	close(w.started)
	<-w.gate
	b, e := os.ReadFile(in)
	if e != nil {
		return e
	}
	return os.WriteFile(out, b, 0600)
}
func TestDocumentDeliveryRevokedDuringPreparation(t *testing.T) {
	w := blockingWatermarker{make(chan struct{}), make(chan struct{})}
	d := setupDelivery(t, w)
	require.NoError(t, d.perms.Grant(context.Background(), &domain.AccessGrant{ID: "grant", UserID: "viewer", ResourceType: domain.ResourceDocument, ResourceID: "pdf", AccessLevel: domain.AccessDownload, GrantedBy: "staff"}))
	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- d.get("/download", "viewer") }()
	<-w.started
	require.NoError(t, d.perms.Revoke(context.Background(), "grant"))
	close(w.gate)
	r := <-done
	require.Equal(t, 404, r.Code, r.Body.String())
	require.NotContains(t, r.Body.String(), "%PDF")
}

type countingWMRepo struct {
	repository.WatermarkRepository
	reads atomic.Int32
}

func (r *countingWMRepo) GetConfig(ctx context.Context) (*domain.WatermarkConfig, error) {
	r.reads.Add(1)
	return r.WatermarkRepository.GetConfig(ctx)
}
func TestDocumentDeliverySettingsSnapshot(t *testing.T) {
	d := setupDelivery(t, nil)
	r := &countingWMRepo{WatermarkRepository: d.wm}
	d.handler.wmRepo = r
	result := d.get("/download", "staff")
	require.Equal(t, 200, result.Code)
	require.Equal(t, int32(1), r.reads.Load(), "one settings snapshot must drive the whole request")
}
