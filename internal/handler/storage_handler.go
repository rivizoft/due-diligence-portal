package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/witfoo/due-diligence-portal/internal/domain"
	"github.com/witfoo/due-diligence-portal/internal/middleware"
	storesvc "github.com/witfoo/due-diligence-portal/internal/service/storage"
	"github.com/witfoo/due-diligence-portal/pkg/response"
)

// StorageHandler accepts write-only credentials and returns only safe settings.
type StorageHandler struct {
	service *storesvc.Service
	audit   *middleware.AuditLogger
}

// NewStorageHandler creates the administrator storage settings API.
func NewStorageHandler(service *storesvc.Service, audit *middleware.AuditLogger) *StorageHandler {
	return &StorageHandler{service: service, audit: audit}
}

// RegisterRoutes installs the three administrator-only endpoints.
func (h *StorageHandler) RegisterRoutes(g *echo.Group) {
	admin := middleware.RequireRole(domain.RoleAdmin)
	g.GET("/storage", h.Get, admin)
	g.PUT("/storage", h.Update, admin)
	g.POST("/storage/test", h.Test, admin)
}

// Get returns saved settings without plaintext or encrypted credentials.
func (h *StorageHandler) Get(c echo.Context) error {
	settings, err := h.service.Settings(c.Request().Context())
	if err != nil {
		return storageResponseError(c, err)
	}
	return response.OK(c, "Storage settings retrieved", settings)
}

// Update validates and saves the submitted connection settings.
func (h *StorageHandler) Update(c echo.Context) error {
	var req storesvc.SaveRequest
	if err := c.Bind(&req); err != nil {
		return response.BadRequest(c, "invalid request body")
	}
	settings, err := h.service.Update(c.Request().Context(), req)
	if err != nil {
		h.audit.LogFromContext(c, "storage.update_failed", "storage", "default", "", "failed")
		return storageResponseError(c, err)
	}
	h.audit.LogFromContext(c, "storage.updated", "storage", "default", "", "saved")
	return response.OK(c, "Storage settings saved", settings)
}

// Test verifies object upload/read/deletion without persisting the form.
func (h *StorageHandler) Test(c echo.Context) error {
	var req storesvc.SaveRequest
	if err := c.Bind(&req); err != nil {
		return response.BadRequest(c, "invalid request body")
	}
	if err := h.service.Test(c.Request().Context(), req); err != nil {
		h.audit.LogFromContext(c, "storage.test_failed", "storage", "default", "", "failed")
		return storageResponseError(c, err)
	}
	h.audit.LogFromContext(c, "storage.tested", "storage", "default", "", "passed")
	return response.OK(c, "Connection verified: upload, read and delete succeeded", nil)
}
func isStorageError(err error) bool {
	var validation *storesvc.ValidationError
	var operation *storesvc.OperationError
	return errors.As(err, &validation) || errors.As(err, &operation) || errors.Is(err, storesvc.ErrKey) || errors.Is(err, storesvc.ErrContent) || errors.Is(err, storesvc.ErrLocationLocked) || errors.Is(err, storesvc.ErrUnavailable)
}
func storageResponseError(c echo.Context, err error) error {
	var validation *storesvc.ValidationError
	var operation *storesvc.OperationError
	switch {
	case errors.As(err, &validation):
		return response.BadRequest(c, validation.Message)
	case errors.Is(err, storesvc.ErrLocationLocked):
		return response.Conflict(c, storesvc.ErrLocationLocked.Error())
	case errors.As(err, &operation):
		status := http.StatusServiceUnavailable
		if operation.Timeout {
			status = http.StatusGatewayTimeout
		}
		return deliveryStatus(c, status, operation.Error())
	case errors.Is(err, storesvc.ErrKey):
		return deliveryStatus(c, http.StatusServiceUnavailable, storesvc.ErrKey.Error())
	case errors.Is(err, storesvc.ErrContent):
		return deliveryStatus(c, http.StatusBadGateway, storesvc.ErrContent.Error())
	case errors.Is(err, storesvc.ErrUnavailable):
		return deliveryStatus(c, http.StatusServiceUnavailable, storesvc.ErrUnavailable.Error())
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return deliveryStatus(c, http.StatusGatewayTimeout, "Storage request timed out or was canceled. Please try again.")
	default:
		return response.InternalError(c)
	}
}
