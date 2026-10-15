package middleware

import (
	"context"

	"github.com/labstack/echo/v4"
	"github.com/witfoo/due-diligence-portal/internal/domain"
	"github.com/witfoo/due-diligence-portal/pkg/response"
)

// NDAStatusRepository contains only the reads needed to enforce the NDA gate.
type NDAStatusRepository interface {
	IsExempt(context.Context, string) (bool, error)
	ListTemplates(context.Context) ([]*domain.NDATemplate, error)
	HasSigned(context.Context, string, string) (bool, error)
}

// RequireNDA enforces the data-room gate server-side, including direct API calls.
// An investor may proceed with an exemption or a signature on an active NDA.
// With no active NDA there is no agreement to require. Repository errors fail
// closed. Authentication must run before this middleware.
func RequireNDA(repo NDAStatusRepository) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			role := GetUserRole(c)
			if role == domain.RoleAdmin || role == domain.RoleCompanyMember {
				return next(c)
			}
			if role != domain.RoleInvestor || GetUserID(c) == "" {
				return response.Unauthorized(c, "invalid session")
			}
			ctx, id := c.Request().Context(), GetUserID(c)
			exempt, err := repo.IsExempt(ctx, id)
			if err != nil {
				return response.InternalError(c)
			}
			if exempt {
				return next(c)
			}
			templates, err := repo.ListTemplates(ctx)
			if err != nil {
				return response.InternalError(c)
			}
			required := false
			for _, tmpl := range templates {
				if !tmpl.IsActive {
					continue
				}
				required = true
				signed, err := repo.HasSigned(ctx, id, tmpl.ID)
				if err != nil {
					return response.InternalError(c)
				}
				if signed {
					return next(c)
				}
			}
			if required {
				return response.Forbidden(c, "NDA signature required")
			}
			return next(c)
		}
	}
}
