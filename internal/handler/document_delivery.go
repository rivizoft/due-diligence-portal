package handler

import (
	"context"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/witfoo/due-diligence-portal/internal/domain"
	"github.com/witfoo/due-diligence-portal/internal/middleware"
	pdfsvc "github.com/witfoo/due-diligence-portal/internal/service/pdf"
	"github.com/witfoo/due-diligence-portal/pkg/response"
	"github.com/witfoo/due-diligence-portal/pkg/sanitize"
	"mime"
	"net/http"
	"os"
	"strconv"
	"time"
)

var errDeliveryParameter = errors.New("invalid version or page number")

func (h *DocumentHandler) deliveryContext(c echo.Context) context.CancelFunc {
	timeout := 60 * time.Second
	if h.pdfSvc != nil {
		timeout = h.pdfSvc.Timeout()
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), timeout)
	c.SetRequest(c.Request().WithContext(ctx))
	c.Response().Header().Set("Cache-Control", "private, no-store")
	return cancel
}
func (h *DocumentHandler) deliverySource(c echo.Context, level string) (*domain.Document, *domain.DocumentVersion, error) {
	d, err := h.docRepo.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return nil, nil, err
	}
	ok, err := h.canAccessDocument(c, d, level)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, domain.ErrDocumentNotFound
	}
	n := d.CurrentVersion
	if c.Param("version") != "" {
		n, err = strconv.Atoi(c.Param("version"))
		if err != nil || n < 1 {
			return nil, nil, errDeliveryParameter
		}
	}
	v, err := h.docRepo.GetVersionMetadata(c.Request().Context(), d.ID, n)
	return d, v, err
}
func (h *DocumentHandler) recheck(c echo.Context, id, level string) error {
	d, err := h.docRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	ok, err := h.canAccessDocument(c, d, level)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrDocumentNotFound
	}
	return nil
}
func deliveryError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrDocumentNotFound):
		return response.NotFound(c, "document not found")
	case errors.Is(err, domain.ErrVersionNotFound):
		return response.NotFound(c, "version not found")
	case errors.Is(err, errDeliveryParameter):
		return response.BadRequest(c, errDeliveryParameter.Error())
	case errors.Is(err, pdfsvc.ErrTooLarge):
		return response.TooLarge(c, "PDF exceeds the configured preparation size limit")
	case errors.Is(err, pdfsvc.ErrBusy):
		c.Response().Header().Set("Retry-After", "3")
		return deliveryStatus(c, 503, "PDF preparation is busy. Please try again.")
	case errors.Is(err, pdfsvc.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return deliveryStatus(c, 504, "PDF preparation timed out. Please try again.")
	case errors.Is(err, pdfsvc.ErrUnsupported), errors.Is(err, pdfsvc.ErrRender):
		return deliveryStatus(c, 422, "This PDF cannot be prepared. It may be damaged, encrypted or unsupported.")
	default:
		return response.InternalError(c)
	}
}
func deliveryStatus(c echo.Context, status int, message string) error {
	return c.JSON(status, response.ErrorResponse{Success: false, Error: message, Timestamp: time.Now().UTC().Format(time.RFC3339)})
}
func (h *DocumentHandler) prepared(c echo.Context, d *domain.Document, v *domain.DocumentVersion) (pdfsvc.Artifact, error) {
	if h.pdfSvc == nil || h.wmRepo == nil {
		return pdfsvc.Artifact{}, pdfsvc.ErrRender
	}
	cfg, err := h.wmRepo.GetConfig(c.Request().Context())
	if err != nil {
		return pdfsvc.Artifact{}, err
	}
	return h.prepareConfigured(c, d, v, *cfg)
}
func (h *DocumentHandler) prepareConfigured(c echo.Context, d *domain.Document, v *domain.DocumentVersion, cfg domain.WatermarkConfig) (pdfsvc.Artifact, error) {
	if h.pdfSvc == nil {
		return pdfsvc.Artifact{}, pdfsvc.ErrRender
	}
	src := pdfsvc.Source{DocumentID: d.ID, DocumentName: d.Name, Version: v.VersionNumber, Checksum: v.ChecksumSHA256, Size: v.FileSize, MimeType: v.MimeType}
	id := pdfsvc.Identity{ID: middleware.GetUserID(c), Email: middleware.GetUserEmail(c), Name: middleware.GetUserName(c)}
	return h.pdfSvc.Prepare(c.Request().Context(), src, id, cfg, time.Now(), func(ctx context.Context) ([]byte, error) {
		loaded, err := h.docRepo.GetVersion(ctx, d.ID, v.VersionNumber)
		if err != nil {
			return nil, err
		}
		return loaded.FileData, nil
	})
}
func (h *DocumentHandler) Preview(c echo.Context) error { return h.deliverFile(c, true) }
func (h *DocumentHandler) deliverFile(c echo.Context, inline bool) error {
	cancel := h.deliveryContext(c)
	defer cancel()
	d, v, err := h.deliverySource(c, domain.AccessDownload)
	if err != nil {
		return deliveryError(c, err)
	}
	if inline && v.MimeType != "application/pdf" {
		return deliveryError(c, pdfsvc.ErrUnsupported)
	}
	var artifact pdfsvc.Artifact
	var data []byte
	enabled := false
	var watermarkConfig *domain.WatermarkConfig
	if v.MimeType == "application/pdf" {
		if h.wmRepo == nil {
			return deliveryError(c, pdfsvc.ErrRender)
		}
		cfg, e := h.wmRepo.GetConfig(c.Request().Context())
		if e != nil {
			return deliveryError(c, e)
		}
		enabled = cfg.Enabled
		watermarkConfig = cfg
	}
	if enabled {
		artifact, err = h.prepareConfigured(c, d, v, *watermarkConfig)
		if err != nil {
			return deliveryError(c, err)
		}
		defer artifact.Release()
	} else {
		loaded, e := h.docRepo.GetVersion(c.Request().Context(), d.ID, v.VersionNumber)
		if e != nil {
			return deliveryError(c, e)
		}
		data = loaded.FileData
	}
	if err = h.recheck(c, d.ID, domain.AccessDownload); err != nil {
		return deliveryError(c, err)
	}
	disposition := "attachment"
	action := domain.AuditDocumentDownloaded
	if inline {
		disposition = "inline"
		action = domain.AuditDocumentViewed
	}
	c.Response().Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": sanitize.FileName(d.Name)}))
	if enabled {
		f, e := os.Open(artifact.Path)
		if e != nil {
			return deliveryError(c, e)
		}
		defer f.Close()
		err = c.Stream(http.StatusOK, v.MimeType, f)
	} else {
		err = c.Blob(http.StatusOK, v.MimeType, data)
	}
	if err == nil {
		h.audit.LogFromContext(c, action, "document", d.ID, d.Name, fmt.Sprintf("version=%d", v.VersionNumber))
	}
	return err
}
func (h *DocumentHandler) PreviewInfo(c echo.Context) error {
	cancel := h.deliveryContext(c)
	defer cancel()
	d, v, err := h.deliverySource(c, domain.AccessView)
	if err != nil {
		return deliveryError(c, err)
	}
	if v.MimeType != "application/pdf" {
		return deliveryError(c, pdfsvc.ErrUnsupported)
	}
	canDownload, err := h.canAccessDocument(c, d, domain.AccessDownload)
	if err != nil {
		return deliveryError(c, err)
	}
	mode := "pdf"
	pages := []pdfsvc.PageInfo{}
	if !canDownload {
		mode = "images"
		a, e := h.prepared(c, d, v)
		if e != nil {
			return deliveryError(c, e)
		}
		defer a.Release()
		info, e := h.pdfSvc.Info(c.Request().Context(), a)
		if e != nil {
			return deliveryError(c, e)
		}
		pages = info.Pages
	}
	if err = h.recheck(c, d.ID, domain.AccessView); err != nil {
		return deliveryError(c, err)
	}
	return response.OK(c, "PDF preview ready", map[string]any{"mode": mode, "version_number": v.VersionNumber, "pages": pages, "name": d.Name})
}
func (h *DocumentHandler) PreviewPage(c echo.Context) error {
	cancel := h.deliveryContext(c)
	defer cancel()
	page, err := strconv.Atoi(c.Param("page"))
	if err != nil || page < 1 {
		return deliveryError(c, errDeliveryParameter)
	}
	d, v, err := h.deliverySource(c, domain.AccessView)
	if err != nil {
		return deliveryError(c, err)
	}
	a, err := h.prepared(c, d, v)
	if err != nil {
		return deliveryError(c, err)
	}
	defer a.Release()
	image, err := h.pdfSvc.Page(c.Request().Context(), a, page)
	if err != nil {
		return deliveryError(c, err)
	}
	defer image.Release()
	if err = h.recheck(c, d.ID, domain.AccessView); err != nil {
		return deliveryError(c, err)
	}
	f, err := os.Open(image.Path)
	if err != nil {
		return deliveryError(c, err)
	}
	defer f.Close()
	err = c.Stream(200, "image/png", f)
	if err == nil {
		h.audit.LogFromContext(c, domain.AuditDocumentViewed, "document", d.ID, d.Name, fmt.Sprintf("version=%d page=%d", v.VersionNumber, page))
	}
	return err
}
