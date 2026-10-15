package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
	"github.com/rivizoft/due-diligence-portal/internal/repository"
)

func TestDocumentSecurity_DefaultRequiresGrant(t *testing.T) {
	e, auth, admin := setupDocumentTest(t)
	id := uploadDocument(t, e, admin, "cat-financials")
	investor := loginInvestor(t, auth, admin)
	require.Equal(t, 404, documentRequest(e, investor, "GET", "/documents/"+id+"/download").Code)
}

func TestDocumentSecurity_UnknownAccessModeFailsClosed(t *testing.T) {
	for _, mode := range []string{"", "grant", "GRANTED", " all"} {
		t.Run(mode, func(t *testing.T) {
			setInvestorAccessMode(t, mode)
			e, auth, admin := setupDocumentTest(t)
			id := uploadDocument(t, e, admin, "cat-financials")
			investor := loginInvestor(t, auth, admin)
			require.Equal(t, 404, documentRequest(e, investor, "GET", "/documents/"+id+"/download").Code)
		})
	}
}

func TestDocumentSecurity_NDAGate(t *testing.T) {
	e, auth, admin, db := setupDocumentTestDB(t)
	setInvestorAccessMode(t, "granted")
	id := uploadDocument(t, e, admin, "cat-financials")
	investor := loginInvestor(t, auth, admin)
	ic, err := auth.ValidateToken(investor)
	require.NoError(t, err)
	ac, err := auth.ValidateToken(admin)
	require.NoError(t, err)
	ctx := context.Background()
	nda := repository.NewNDARepository(db)
	require.NoError(t, nda.CreateTemplate(ctx, &domain.NDATemplate{ID: "required", Name: "NDA", Content: "Confidential", IsActive: true, Version: 1, CreatedBy: ac.UserID}))
	require.NoError(t, repository.NewPermissionRepository(db).Grant(ctx, &domain.AccessGrant{ID: "download", UserID: ic.UserID, ResourceType: domain.ResourceDocument, ResourceID: id, AccessLevel: domain.AccessDownload, GrantedBy: ac.UserID}))
	for _, suffix := range []string{"", "/download", "/versions/1", "/preview", "/preview-info", "/preview-pages/1", "/versions/1/preview", "/versions/1/preview-info", "/versions/1/preview-pages/1"} {
		w := documentRequest(e, investor, "GET", "/documents/"+id+suffix)
		require.Equal(t, 403, w.Code, "unsigned investor: %s", suffix)
		require.NotContains(t, w.Body.String(), "fake pdf content")
	}
	require.Equal(t, 403, documentRequest(e, investor, "GET", "/documents").Code)
	r := httptest.NewRequest("POST", "/api/v1/documents/search", strings.NewReader(`{"query":"Test"}`))
	r.Header.Set("Authorization", "Bearer "+investor)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	require.Equal(t, 403, w.Code)
	// Staff access is unaffected; an administrator may explicitly exempt an investor.
	require.Equal(t, 200, documentRequest(e, admin, "GET", "/documents/"+id+"/download").Code)
	require.NoError(t, nda.UpsertExemption(ctx, &domain.NDAExemption{UserID: ic.UserID, GrantedBy: ac.UserID, Reason: "Executed externally"}))
	require.Equal(t, 200, documentRequest(e, investor, "GET", "/documents/"+id+"/download").Code)
	require.NoError(t, nda.DeleteExemption(ctx, ic.UserID))
	require.Equal(t, 403, documentRequest(e, investor, "GET", "/documents/"+id+"/download").Code)
	require.NoError(t, nda.CreateSignature(ctx, &domain.NDASignature{ID: "signed", UserID: ic.UserID, TemplateID: "required", SignerName: "Investor", SignerEmail: ic.Email, IPAddress: "127.0.0.1"}))
	require.Equal(t, 200, documentRequest(e, investor, "GET", "/documents/"+id+"/download").Code)
}
