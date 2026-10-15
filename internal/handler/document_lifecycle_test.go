package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
	"github.com/rivizoft/due-diligence-portal/internal/repository"
)

func documentRequest(e *echo.Echo, token, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1"+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func uploadTestVersion(t *testing.T, e *echo.Echo, token, id, content string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "updated.pdf")
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("change_note", "Updated document"))
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents/"+id+"/versions", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

func TestDocumentLifecycle_DeleteVersions(t *testing.T) {
	e, _, token := setupDocumentTest(t)
	id := uploadDocument(t, e, token, "cat-financials")
	path := "/documents/" + id
	uploadTestVersion(t, e, token, id, "%PDF-1.7\nsecond")
	uploadTestVersion(t, e, token, id, "%PDF-1.7\nthird")
	require.Equal(t, http.StatusOK, documentRequest(e, token, "DELETE", path+"/versions/3").Code)
	rec := documentRequest(e, token, "GET", path+"/download")
	require.Equal(t, "%PDF-1.7\nsecond", rec.Body.String())
	require.Equal(t, "application/pdf", rec.Header().Get("Content-Type"))
	require.Equal(t, http.StatusNotFound, documentRequest(e, token, "GET", path+"/versions/3").Code)
	uploadTestVersion(t, e, token, id, "%PDF-1.7\nfourth")
	require.Equal(t, http.StatusOK, documentRequest(e, token, "GET", path+"/versions/4").Code)
	require.Equal(t, http.StatusOK, documentRequest(e, token, "DELETE", path+"/versions/2").Code)
	require.Equal(t, "%PDF-1.7\nfourth", documentRequest(e, token, "GET", path+"/download").Body.String())
	require.Equal(t, http.StatusOK, documentRequest(e, token, "DELETE", path+"/versions/4").Code)
	require.Equal(t, http.StatusConflict, documentRequest(e, token, "DELETE", path+"/versions/1").Code)
	require.Equal(t, http.StatusNotFound, documentRequest(e, token, "DELETE", path+"/versions/99").Code)
	require.Equal(t, http.StatusBadRequest, documentRequest(e, token, "DELETE", path+"/versions/0").Code)
}

func TestDocumentLifecycle_ViewOnly(t *testing.T) {
	for _, mode := range []string{"granted", "all"} {
		for _, resourceType := range []string{domain.ResourceDocument, domain.ResourceCategory} {
			t.Run(mode+"/"+resourceType, func(t *testing.T) {
				e, auth, admin, db := setupDocumentTestDB(t)
				setInvestorAccessMode(t, mode)
				id := uploadDocument(t, e, admin, "cat-financials")
				uploadTestVersion(t, e, admin, id, "%PDF-1.7\npreview")
				investor := loginInvestor(t, auth, admin)
				claims, err := auth.ValidateToken(investor)
				require.NoError(t, err)
				adminClaims, err := auth.ValidateToken(admin)
				require.NoError(t, err)
				resourceID := id
				if resourceType == domain.ResourceCategory {
					resourceID = "cat-financials"
				}
				perms := repository.NewPermissionRepository(db)
				require.NoError(t, perms.Grant(context.Background(), &domain.AccessGrant{
					ID: "view-only", UserID: claims.UserID, ResourceType: resourceType, ResourceID: resourceID,
					AccessLevel: domain.AccessView, GrantedBy: adminClaims.UserID,
				}))
				path := "/documents/" + id
				for _, endpoint := range []string{path} {
					rec := documentRequest(e, investor, "GET", endpoint)
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
					var envelope struct {
						Data struct {
							Permissions struct {
								Download bool `json:"can_download"`
								Manage   bool `json:"can_manage"`
							} `json:"permissions"`
						} `json:"data"`
					}
					require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
					require.False(t, envelope.Data.Permissions.Download)
					require.False(t, envelope.Data.Permissions.Manage)
				}
				for _, suffix := range []string{"/download", "/versions/2", "/preview", "/versions/2/preview"} {
					require.Equal(t, http.StatusNotFound, documentRequest(e, investor, "GET", path+suffix).Code)
				}
				for _, endpoint := range []string{path, path + "/versions/2"} {
					require.Equal(t, http.StatusForbidden, documentRequest(e, investor, "DELETE", endpoint).Code)
				}
				require.Equal(t, http.StatusNotFound, documentRequest(e, investor, "GET", path+"/versions/1/preview").Code)
				require.NoError(t, perms.Revoke(context.Background(), "view-only"))
				if mode == "granted" {
					require.Equal(t, http.StatusNotFound, documentRequest(e, investor, "GET", path+"/preview").Code)
				}
			})
		}
	}
}

func TestDocumentLifecycle_ArchivedCannotBeRead(t *testing.T) {
	e, _, token := setupDocumentTest(t)
	id := uploadDocument(t, e, token, "cat-financials")
	path := "/documents/" + id
	require.Equal(t, http.StatusOK, documentRequest(e, token, "DELETE", path).Code)
	for _, suffix := range []string{"", "/download", "/versions/1", "/preview"} {
		t.Run(fmt.Sprintf("GET %s", suffix), func(t *testing.T) {
			require.Equal(t, http.StatusNotFound, documentRequest(e, token, "GET", path+suffix).Code)
		})
	}
}
