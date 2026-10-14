package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/witfoo/due-diligence-portal/internal/domain"
	"github.com/witfoo/due-diligence-portal/internal/middleware"
	"github.com/witfoo/due-diligence-portal/internal/repository"
	"github.com/witfoo/due-diligence-portal/internal/service"
	storesvc "github.com/witfoo/due-diligence-portal/internal/service/storage"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type handlerObjectStore struct {
	objects     map[string][]byte
	reads       int
	deleted     int
	putError    error
	deleteError error
}

func (s *handlerObjectStore) Put(_ context.Context, b, k string, data []byte, _ string) error {
	if s.putError != nil {
		return s.putError
	}
	s.objects[b+"/"+k] = append([]byte{}, data...)
	return nil
}
func (s *handlerObjectStore) Get(_ context.Context, b, k string, _ int64) ([]byte, error) {
	s.reads++
	data, ok := s.objects[b+"/"+k]
	if !ok {
		return nil, storesvc.ErrContent
	}
	return data, nil
}
func (s *handlerObjectStore) Delete(_ context.Context, b, k string) error {
	if s.deleteError != nil {
		return s.deleteError
	}
	s.deleted++
	delete(s.objects, b+"/"+k)
	return nil
}
func setupStorageHTTP(t *testing.T) (*echo.Echo, *service.AuthService, string, *repository.DB, *storesvc.Service, *handlerObjectStore) {
	t.Helper()
	e, auth, token, db := setupDocumentTestDB(t)
	docs := repository.NewDocumentRepository(db)
	m := &handlerObjectStore{objects: map[string][]byte{}}
	svc := storesvc.NewService(repository.NewStorageRepository(db), docs, filepath.Join(t.TempDir(), "storage.key"), func(_ *domain.StorageConfig, _ storesvc.Credentials) (storesvc.Client, error) { return m, nil })
	audit := middleware.NewAuditLogger(db)
	group := e.Group("/api/v1", middleware.JWTAuth(auth))
	NewStorageHandler(svc, audit).RegisterRoutes(group)
	NewDocumentHandler(docs, repository.NewPermissionRepository(db), audit, repository.NewWatermarkRepository(db), nil, svc).RegisterRoutes(group)
	return e, auth, token, db, svc, m
}
func storageHTTP(t *testing.T, e *echo.Echo, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}
func storageHTTPConfig() storesvc.SaveRequest {
	return storesvc.SaveRequest{Enabled: true, Bucket: "private-bucket", Region: "eu-central-1", AccessKeyID: "http-access", SecretAccessKey: "http-secret", KeyPrefix: "documents"}
}
func TestStorageHTTPAdminOnly(t *testing.T) {
	e, auth, admin, _, _, _ := setupStorageHTTP(t)
	investor := loginInvestor(t, auth, admin)
	claims, err := auth.ValidateToken(admin)
	require.NoError(t, err)
	invite, err := auth.CreateInvite(context.Background(), "company@storage.test", domain.RoleCompanyMember, claims.UserID)
	require.NoError(t, err)
	company, err := auth.Register(context.Background(), invite.Token, "Company", "password123")
	require.NoError(t, err)
	for _, route := range []struct{ method, path string }{{"GET", "/storage"}, {"PUT", "/storage"}, {"POST", "/storage/test"}} {
		require.Equal(t, 401, storageHTTP(t, e, "", route.method, route.path, storageHTTPConfig()).Code)
		for _, token := range []string{investor, company.AccessToken} {
			require.Equal(t, 403, storageHTTP(t, e, token, route.method, route.path, storageHTTPConfig()).Code)
		}
	}
}
func TestStorageHTTPSaveMasksAndValidates(t *testing.T) {
	e, _, admin, db, _, m := setupStorageHTTP(t)
	config := storageHTTPConfig()
	require.Equal(t, 200, storageHTTP(t, e, admin, "POST", "/storage/test", config).Code)
	got := storageHTTP(t, e, admin, "GET", "/storage", nil)
	require.NotContains(t, got.Body.String(), `"enabled":true`)
	got = storageHTTP(t, e, admin, "PUT", "/storage", config)
	require.Equal(t, 200, got.Code, got.Body.String())
	require.NotContains(t, got.Body.String(), "http-secret")
	require.NotContains(t, got.Body.String(), "http-access")
	require.NotContains(t, got.Body.String(), `"secret_access_key"`)
	require.Empty(t, m.objects)
	config.AccessKeyID = ""
	config.SecretAccessKey = ""
	require.Equal(t, 200, storageHTTP(t, e, admin, "PUT", "/storage", config).Code)
	config.SecretAccessKey = "only-half"
	require.Equal(t, 400, storageHTTP(t, e, admin, "PUT", "/storage", config).Code)
	var audit string
	require.NoError(t, db.QueryRow(`SELECT COALESCE(group_concat(details), '') FROM audit_log WHERE action LIKE 'storage.%'`).Scan(&audit))
	require.NotContains(t, audit, "http-secret")
}
func TestS3HTTPDocumentLifecycle(t *testing.T) {
	e, auth, admin, db, _, m := setupStorageHTTP(t)
	setInvestorAccessMode(t, "granted")
	local := uploadDocument(t, e, admin, "cat-financials")
	config := storageHTTPConfig()
	require.Equal(t, 200, storageHTTP(t, e, admin, "PUT", "/storage", config).Code)
	remote := uploadDocument(t, e, admin, "cat-financials")
	uploadTestVersion(t, e, admin, remote, "second remote")
	path := "/documents/" + remote
	require.Equal(t, "second remote", documentRequest(e, admin, "GET", path+"/download").Body.String())
	detail := documentRequest(e, admin, "GET", path)
	require.NotContains(t, detail.Body.String(), "private-bucket")
	require.NotContains(t, detail.Body.String(), "object_key")
	investor := loginInvestor(t, auth, admin)
	before := m.reads
	require.Equal(t, 404, documentRequest(e, investor, "GET", path+"/download").Code)
	require.Equal(t, before, m.reads)
	config.Enabled = false
	config.AccessKeyID = ""
	config.SecretAccessKey = ""
	m.putError = errors.New("private SDK detail http-secret")
	require.Equal(t, 200, storageHTTP(t, e, admin, "PUT", "/storage", config).Code)
	require.Equal(t, "second remote", documentRequest(e, admin, "GET", path+"/download").Body.String())
	require.Equal(t, "fake pdf content", documentRequest(e, admin, "GET", "/documents/"+local+"/download").Body.String())
	config.Bucket = "other-bucket"
	require.Equal(t, 409, storageHTTP(t, e, admin, "PUT", "/storage", config).Code)
	m.deleteError = errors.New("offline")
	require.Equal(t, 200, documentRequest(e, admin, "DELETE", path+"/versions/2").Code)
	require.Equal(t, "fake pdf content", documentRequest(e, admin, "GET", path+"/download").Body.String())
	deleted := m.deleted
	require.Equal(t, 409, documentRequest(e, admin, "DELETE", path+"/versions/1").Code)
	require.Equal(t, deleted, m.deleted)
	var jobs int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM storage_cleanup_jobs`).Scan(&jobs))
	require.Equal(t, 1, jobs)
}
func TestStorageHTTPUpstreamErrorsAreSanitized(t *testing.T) {
	e, _, admin, _, _, m := setupStorageHTTP(t)
	m.putError = errors.New("http-secret private SDK response")
	res := storageHTTP(t, e, admin, "POST", "/storage/test", storageHTTPConfig())
	require.Equal(t, 503, res.Code)
	require.NotContains(t, res.Body.String(), "http-secret")
	require.NotContains(t, res.Body.String(), "SDK response")
	m.putError = context.DeadlineExceeded
	require.Equal(t, 504, storageHTTP(t, e, admin, "POST", "/storage/test", storageHTTPConfig()).Code)
}
