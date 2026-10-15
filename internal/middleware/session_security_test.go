package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/witfoo/due-diligence-portal/internal/domain"
	"github.com/witfoo/due-diligence-portal/internal/repository"
	"github.com/witfoo/due-diligence-portal/internal/service"
)

func TestJWTAuth_RevokesStaleSessions(t *testing.T) {
	for _, change := range []string{"deactivate", "password reset", "demote", "delete", "database unavailable"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			db, err := repository.New(":memory:")
			require.NoError(t, err)
			require.NoError(t, db.Migrate())
			t.Cleanup(func() { db.Close() })
			repo := repository.NewUserRepository(db)
			auth := service.NewAuthService(repo, testSecret)
			hash, err := service.HashPassword("password123")
			require.NoError(t, err)
			u := &domain.User{ID: "victim", Email: "victim@example.com", Name: "Victim", PasswordHash: hash, Role: domain.RoleAdmin, IsActive: true}
			require.NoError(t, repo.Create(ctx, u))
			login, err := auth.Login(ctx, u.Email, "password123")
			require.NoError(t, err)
			e := echo.New()
			e.GET("/private", func(c echo.Context) error { return c.String(200, "confidential document") }, JWTAuth(auth), RequireRole(domain.RoleAdmin))
			request := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "/private", nil)
				r.Header.Set("Authorization", "Bearer "+login.AccessToken)
				w := httptest.NewRecorder()
				e.ServeHTTP(w, r)
				return w
			}
			require.Equal(t, 200, request().Code)
			switch change {
			case "deactivate":
				require.NoError(t, repo.Deactivate(ctx, u.ID))
			case "password reset":
				_, err = auth.SetPassword(ctx, u.ID, "replacement123")
				require.NoError(t, err)
			case "demote":
				u.Role = domain.RoleInvestor
				require.NoError(t, repo.Update(ctx, u))
			case "delete":
				_, err = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", u.ID)
				require.NoError(t, err)
			case "database unavailable":
				require.NoError(t, db.Close())
			}
			w := request()
			require.Equal(t, 401, w.Code, "old token must stop authorizing immediately")
			require.NotContains(t, w.Body.String(), "confidential document")
		})
	}
}
