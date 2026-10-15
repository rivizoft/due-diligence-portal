package handler

import (
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
	"github.com/rivizoft/due-diligence-portal/internal/middleware"
	"github.com/rivizoft/due-diligence-portal/internal/repository"
	"github.com/rivizoft/due-diligence-portal/pkg/response"
)

func (h *DocumentHandler) listDocuments(c echo.Context, query, message string) error {
	limit, offset := 50, 0
	if n, err := strconv.Atoi(c.QueryParam("limit")); err == nil && n > 0 && n <= 100 {
		limit = n
	}
	if n, err := strconv.Atoi(c.QueryParam("offset")); err == nil && n >= 0 {
		offset = n
	}
	filter := repository.DocumentFilter{CategoryID: c.QueryParam("category_id"), Query: query}
	role := middleware.GetUserRole(c)
	if role != domain.RoleAdmin && role != domain.RoleCompanyMember {
		filter.ViewerID = middleware.GetUserID(c)
		filter.OpenAccess = investorAccessOpen()
	}
	docs, total, err := h.docRepo.Find(c.Request().Context(), filter, limit, offset)
	if err != nil {
		return response.InternalError(c)
	}
	if err := h.setDownloadPermissions(c, docs); err != nil {
		return response.InternalError(c)
	}
	return response.OKWithMeta(c, message, docs, &response.Meta{
		Count: len(docs), Total: total, Page: offset/limit + 1, PageSize: limit,
		HasMore: offset < total && limit < total-offset,
	})
}
