package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
	"github.com/rivizoft/due-diligence-portal/internal/repository"
	"github.com/rivizoft/due-diligence-portal/pkg/response"
)

func TestDownloadPreservesCyrillicFilename(t *testing.T) {
	d := setupDelivery(t, nil)
	doc, err := d.docs.GetByID(context.Background(), "pdf")
	require.NoError(t, err)
	doc.Name = "Пояснение по категориям.pdf"
	require.NoError(t, d.docs.Update(context.Background(), doc))
	for _, route := range []string{"/download", "/preview", "/versions/1"} {
		r := d.get(route, "staff")
		require.Equal(t, 200, r.Code, r.Body.String())
		_, params, err := mime.ParseMediaType(r.Header().Get("Content-Disposition"))
		require.NoError(t, err)
		require.Equal(t, doc.Name, params["filename"])
		require.Contains(t, strings.ToLower(r.Header().Get("Content-Disposition")), "filename*=utf-8''")
	}
}

func TestDocumentPaginationCountsVisibleResultsBeforeSlicing(t *testing.T) {
	for _, mode := range []string{"granted", "all"} {
		t.Run(mode, func(t *testing.T) {
			setInvestorAccessMode(t, mode)
			e, auth, admin, db := setupDocumentTestDB(t)
			investor := loginInvestor(t, auth, admin)
			ac, err := auth.ValidateToken(admin)
			require.NoError(t, err)
			ic, err := auth.ValidateToken(investor)
			require.NoError(t, err)
			ctx := context.Background()
			docs := repository.NewDocumentRepository(db)
			for i := 0; i < 31; i++ {
				category := "cat-financials"
				if i%3 == 0 {
					category = "cat-legal"
				}
				require.NoError(t, docs.Create(ctx, &domain.Document{ID: fmt.Sprintf("page-%02d", i), Name: fmt.Sprintf("Needle report %02d", i), CategoryID: category, UploadedBy: ac.UserID, CurrentVersion: 1, MimeType: "application/pdf"}))
			}
			perms := repository.NewPermissionRepository(db)
			require.NoError(t, perms.Grant(ctx, &domain.AccessGrant{ID: "category-view", UserID: ic.UserID, ResourceType: domain.ResourceCategory, ResourceID: "cat-financials", AccessLevel: domain.AccessView, GrantedBy: ac.UserID}))
			expired := time.Now().Add(-time.Hour)
			require.NoError(t, perms.Grant(ctx, &domain.AccessGrant{ID: "expired-view", UserID: ic.UserID, ResourceType: domain.ResourceDocument, ResourceID: "page-00", AccessLevel: domain.AccessView, GrantedBy: ac.UserID, ExpiresAt: &expired}))
			for _, actor := range []struct {
				name, token string
				total       int
			}{{"admin", admin, 31}, {"investor", investor, map[string]int{"granted": 20, "all": 30}[mode]}} {
				for _, search := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/search=%v", actor.name, search), func(t *testing.T) {
						seen := map[string]bool{}
						for page := 0; page < (actor.total+9)/10; page++ {
							path := fmt.Sprintf("/api/v1/documents?limit=10&offset=%d", page*10)
							method, body := "GET", ""
							if search {
								path = fmt.Sprintf("/api/v1/documents/search?limit=10&offset=%d", page*10)
								method, body = "POST", `{"query":"Needle"}`
							}
							req := httptest.NewRequest(method, path, strings.NewReader(body))
							req.Header.Set("Authorization", "Bearer "+actor.token)
							req.Header.Set("Content-Type", "application/json")
							rec := httptest.NewRecorder()
							e.ServeHTTP(rec, req)
							require.Equal(t, 200, rec.Code, rec.Body.String())
							var result struct {
								Data []domain.Document
								Meta response.Meta
							}
							require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
							require.Equal(t, actor.total, result.Meta.Total)
							require.Equal(t, page+1, result.Meta.Page)
							require.Equal(t, 10, result.Meta.PageSize)
							require.Len(t, result.Data, min(10, actor.total-page*10))
							require.Equal(t, (page+1)*10 < actor.total, result.Meta.HasMore)
							for _, doc := range result.Data {
								require.False(t, seen[doc.ID], "document repeated across pages: %s", doc.ID)
								seen[doc.ID] = true
								if actor.name == "investor" {
									require.NotEqual(t, "page-00", doc.ID)
								}
							}
						}
						require.Len(t, seen, actor.total)
					})
				}
			}
		})
	}
}

func TestDocumentSearchPaginationRespectsCategoryAndPageSize(t *testing.T) {
	d := setupDelivery(t, nil)
	for i := 0; i < 100; i++ {
		category := "cat-financials"
		if i >= 60 {
			category = "cat-legal"
		}
		require.NoError(t, d.docs.Create(context.Background(), &domain.Document{ID: fmt.Sprintf("sized-%03d", i), Name: "Needle report", CategoryID: category, UploadedBy: "staff", CurrentVersion: 1, IsArchived: i == 0}))
	}
	for _, size := range []int{10, 20, 30, 50, 100} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/documents/search?category_id=cat-financials&limit=%d&offset=%d", size, size), strings.NewReader(`{"query":"Needle"}`))
			req.Header.Set("Authorization", "staff")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			d.e.ServeHTTP(rec, req)
			require.Equal(t, 200, rec.Code, rec.Body.String())
			var result struct {
				Data []domain.Document
				Meta response.Meta
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
			require.Equal(t, 59, result.Meta.Total)
			require.Equal(t, size, result.Meta.PageSize)
			require.Equal(t, 2, result.Meta.Page)
			require.Len(t, result.Data, max(0, min(size, 59-size)))
			for _, doc := range result.Data {
				require.Equal(t, "cat-financials", doc.CategoryID)
				require.False(t, doc.IsArchived)
			}
		})
	}
}
