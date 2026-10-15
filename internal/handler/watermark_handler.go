package handler

import (
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	pdfsvc "github.com/rivizoft/due-diligence-portal/internal/service/pdf"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
	"github.com/rivizoft/due-diligence-portal/internal/middleware"
	"github.com/rivizoft/due-diligence-portal/internal/repository"
	"github.com/rivizoft/due-diligence-portal/pkg/response"
)

// WatermarkHandler handles watermark configuration endpoints.
type WatermarkHandler struct {
	wmRepo repository.WatermarkRepository
	audit  *middleware.AuditLogger
}

// NewWatermarkHandler creates a new WatermarkHandler.
func NewWatermarkHandler(wmRepo repository.WatermarkRepository, audit *middleware.AuditLogger) *WatermarkHandler {
	return &WatermarkHandler{wmRepo: wmRepo, audit: audit}
}

// RegisterRoutes registers watermark routes on the given group.
func (h *WatermarkHandler) RegisterRoutes(g *echo.Group) {
	adminOnly := middleware.RequireRole(domain.RoleAdmin)
	g.GET("/watermark", h.GetConfig, adminOnly)
	g.PUT("/watermark", h.UpdateConfig, adminOnly)
	g.DELETE("/watermark", h.ResetConfig, adminOnly)
}

// GetConfig handles GET /api/v1/watermark.
func (h *WatermarkHandler) GetConfig(c echo.Context) error {
	config, err := h.wmRepo.GetConfig(c.Request().Context())
	if err != nil {
		return response.InternalError(c)
	}
	return response.OK(c, "Watermark config retrieved", config)
}

type updateWatermarkRequest struct {
	Enabled      *bool    `json:"enabled"`
	TextTemplate *string  `json:"text_template"`
	Position     string   `json:"position"`
	Opacity      *float64 `json:"opacity"`
	FontSize     *int     `json:"font_size"`
	Color        string   `json:"color"`
}

var validPositions = map[string]bool{
	"diagonal": true,
	"top":      true,
	"bottom":   true,
	"center":   true,
}

// UpdateConfig handles PUT /api/v1/watermark.
func (h *WatermarkHandler) UpdateConfig(c echo.Context) error {
	var req updateWatermarkRequest
	if err := c.Bind(&req); err != nil {
		return response.BadRequest(c, "invalid request body")
	}

	config, err := h.wmRepo.GetConfig(c.Request().Context())
	if err != nil {
		return response.InternalError(c)
	}

	if req.Enabled != nil {
		config.Enabled = *req.Enabled
	}
	if req.TextTemplate != nil {
		config.TextTemplate = *req.TextTemplate
	}
	if req.Position != "" {
		if !validPositions[req.Position] {
			return response.BadRequest(c, "invalid position (must be diagonal, top, bottom, or center)")
		}
		config.Position = req.Position
	}
	if req.Opacity != nil {
		if math.IsNaN(*req.Opacity) || math.IsInf(*req.Opacity, 0) || *req.Opacity < 0 || *req.Opacity > 1 {
			return response.BadRequest(c, "opacity must be between 0 and 1")
		}
		config.Opacity = *req.Opacity
	}
	if req.FontSize != nil {
		if *req.FontSize < 6 || *req.FontSize > 72 {
			return response.BadRequest(c, "font_size must be between 6 and 72")
		}
		config.FontSize = *req.FontSize
	}
	if req.Color != "" {
		config.Color = req.Color
	}

	if len(config.TextTemplate) > 2048 {
		return response.BadRequest(c, "text_template must be at most 2048 bytes")
	}
	if config.Enabled && strings.TrimSpace(config.TextTemplate) == "" {
		return response.BadRequest(c, "enabled watermark requires a text template")
	}
	if _, err := pdfsvc.RenderText(config.TextTemplate, pdfsvc.Identity{}, "", time.Now()); err != nil {
		return response.BadRequest(c, err.Error())
	}
	if !regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(config.Color) {
		return response.BadRequest(c, "color must be a six-digit hex color")
	}
	if err := h.wmRepo.UpsertConfig(c.Request().Context(), config); err != nil {
		return response.InternalError(c)
	}

	h.audit.LogFromContext(c, "watermark.updated", "watermark", "default", "", "")
	return response.OK(c, "Watermark config updated", config)
}

// ResetConfig handles DELETE /api/v1/watermark.
func (h *WatermarkHandler) ResetConfig(c echo.Context) error {
	if err := h.wmRepo.ResetConfig(c.Request().Context()); err != nil {
		return response.InternalError(c)
	}

	h.audit.LogFromContext(c, "watermark.reset", "watermark", "default", "", "")

	config, err := h.wmRepo.GetConfig(c.Request().Context())
	if err != nil {
		return response.InternalError(c)
	}
	return response.OK(c, "Watermark config reset to defaults", config)
}
