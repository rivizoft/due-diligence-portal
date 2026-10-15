package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/witfoo/due-diligence-portal/internal/domain"
	"github.com/witfoo/due-diligence-portal/internal/repository"
)

func TestRequireNDA(t *testing.T) {
	for _, tc := range []struct {
		name   string
		role   string
		status int
	}{
		{"unsigned", domain.RoleInvestor, 403},
		{"signed", domain.RoleInvestor, 200},
		{"exempt", domain.RoleInvestor, 200},
		{"retired signature", domain.RoleInvestor, 403},
		{"no active NDA", domain.RoleInvestor, 200},
		{"admin", domain.RoleAdmin, 200},
		{"company member", domain.RoleCompanyMember, 200},
		{"unknown role", "unknown", 401},
		{"exemption lookup failure", domain.RoleInvestor, 500},
		{"template lookup failure", domain.RoleInvestor, 500},
		{"signature lookup failure", domain.RoleInvestor, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := repository.New(":memory:")
			require.NoError(t, err)
			require.NoError(t, db.Migrate())
			t.Cleanup(func() { db.Close() })
			users := repository.NewUserRepository(db)
			require.NoError(t, users.Create(ctx, &domain.User{ID: "u", Email: "u@example.com", Name: "User", PasswordHash: "unused", Role: domain.RoleInvestor, IsActive: true}))
			repo := repository.NewNDARepository(db)
			require.NoError(t, repo.CreateTemplate(ctx, &domain.NDATemplate{ID: "active", Name: "NDA", Content: "Terms", IsActive: tc.name != "no active NDA", Version: 1, CreatedBy: "u"}))
			switch tc.name {
			case "signed", "retired signature":
				templateID := "active"
				if tc.name == "retired signature" {
					templateID = "retired"
					require.NoError(t, repo.CreateTemplate(ctx, &domain.NDATemplate{ID: templateID, Name: "Old NDA", Content: "Old terms", Version: 1, CreatedBy: "u"}))
				}
				require.NoError(t, repo.CreateSignature(ctx, &domain.NDASignature{ID: "sig", UserID: "u", TemplateID: templateID, SignerName: "User", SignerEmail: "u@example.com", IPAddress: "127.0.0.1"}))
			case "exempt":
				require.NoError(t, repo.UpsertExemption(ctx, &domain.NDAExemption{UserID: "u", GrantedBy: "u"}))
			case "exemption lookup failure":
				_, err = db.ExecContext(ctx, "DROP TABLE nda_exemptions")
				require.NoError(t, err)
			case "template lookup failure":
				_, err = db.ExecContext(ctx, "DROP TABLE nda_templates")
				require.NoError(t, err)
			case "signature lookup failure":
				_, err = db.ExecContext(ctx, "DROP TABLE nda_signatures")
				require.NoError(t, err)
			}
			e := echo.New()
			w := httptest.NewRecorder()
			c := e.NewContext(httptest.NewRequest("GET", "/private", nil), w)
			c.Set(ContextKeyUserID, "u")
			c.Set(ContextKeyRole, tc.role)
			err = RequireNDA(repo)(func(c echo.Context) error { return c.String(200, "confidential document") })(c)
			require.NoError(t, err)
			require.Equal(t, tc.status, w.Code)
			if tc.status != 200 {
				require.NotContains(t, w.Body.String(), "confidential document")
			}
		})
	}
}
