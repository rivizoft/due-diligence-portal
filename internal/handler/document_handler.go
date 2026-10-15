package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
	"github.com/rivizoft/due-diligence-portal/internal/middleware"
	"github.com/rivizoft/due-diligence-portal/internal/repository"
	pdfsvc "github.com/rivizoft/due-diligence-portal/internal/service/pdf"
	"github.com/rivizoft/due-diligence-portal/pkg/envconfig"
	"github.com/rivizoft/due-diligence-portal/pkg/response"
	"github.com/rivizoft/due-diligence-portal/pkg/sanitize"
)

const defaultMaxUploadSize int64 = 100 * 1024 * 1024       // 100MB
const absoluteMaxUploadSize int64 = 2 * 1024 * 1024 * 1024 // 2GB sanity cap

// DocumentHandler handles document management endpoints.
type DocumentHandler struct {
	docRepo       repository.DocumentRepository
	permRepo      repository.PermissionRepository
	audit         *middleware.AuditLogger
	maxUploadSize int64
	wmRepo        repository.WatermarkRepository
	pdfSvc        *pdfsvc.Service
	content       VersionContent
}

// NewDocumentHandler creates a new DocumentHandler.
func NewDocumentHandler(docRepo repository.DocumentRepository, permRepo repository.PermissionRepository, audit *middleware.AuditLogger, wmRepo repository.WatermarkRepository, pdfSvc *pdfsvc.Service, content ...VersionContent) *DocumentHandler {
	maxSize := envconfig.GetEnvInt64("DD_MAX_UPLOAD_SIZE", defaultMaxUploadSize)
	if maxSize <= 0 || maxSize > absoluteMaxUploadSize {
		maxSize = defaultMaxUploadSize
	}
	h := &DocumentHandler{docRepo: docRepo, permRepo: permRepo, audit: audit, maxUploadSize: maxSize, wmRepo: wmRepo, pdfSvc: pdfSvc}
	if len(content) > 0 {
		h.content = content[0]
	}
	return h
}

// RegisterRoutes registers document routes on a pre-authenticated group.
func (h *DocumentHandler) RegisterRoutes(g *echo.Group) {
	// Upload routes get a body limit just above the configured max upload so the
	// request body is bounded regardless of the client-declared multipart size.
	uploadLimit := echomw.BodyLimit(fmt.Sprintf("%dB", h.maxUploadSize+1024*1024))
	g.GET("/documents", h.List)
	g.GET("/documents/:id", h.Get)
	g.POST("/documents", h.Upload, middleware.RequireRole(domain.RoleAdmin, domain.RoleCompanyMember), uploadLimit)
	g.PUT("/documents/:id", h.Update, middleware.RequireRole(domain.RoleAdmin, domain.RoleCompanyMember))
	g.DELETE("/documents/:id", h.Archive, middleware.RequireRole(domain.RoleAdmin, domain.RoleCompanyMember))
	g.POST("/documents/:id/versions", h.UploadVersion, middleware.RequireRole(domain.RoleAdmin, domain.RoleCompanyMember), uploadLimit)
	g.GET("/documents/:id/versions/:version", h.DownloadVersion)
	g.GET("/documents/:id/download", h.Download)
	g.GET("/documents/:id/preview", h.Preview)
	g.GET("/documents/:id/preview-info", h.PreviewInfo)
	g.GET("/documents/:id/preview-pages/:page", h.PreviewPage)
	g.GET("/documents/:id/versions/:version/preview", h.Preview)
	g.GET("/documents/:id/versions/:version/preview-info", h.PreviewInfo)
	g.GET("/documents/:id/versions/:version/preview-pages/:page", h.PreviewPage)
	g.DELETE("/documents/:id/versions/:version", h.DeleteVersion, middleware.RequireRole(domain.RoleAdmin, domain.RoleCompanyMember))
	g.POST("/documents/search", h.Search)
}

// canAccessDocument reports whether the current user may access the given document
// at the required level. Admin and company members have full access; investors must
// hold a matching grant on the document OR on its category. This is the single
// authorization helper for every per-document read path.
func (h *DocumentHandler) canAccessDocument(c echo.Context, doc *domain.Document, level string) (bool, error) {
	return documentAccessAllowed(c.Request().Context(), h.permRepo,
		middleware.GetUserID(c), middleware.GetUserRole(c), doc, level)
}

// List handles GET /documents.
func (h *DocumentHandler) List(c echo.Context) error {
	return h.listDocuments(c, "", "Documents retrieved")
}

// Get handles GET /documents/:id.
func (h *DocumentHandler) Get(c echo.Context) error {
	id := c.Param("id")
	doc, err := h.docRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		if err == domain.ErrDocumentNotFound {
			return response.NotFound(c, "document not found")
		}
		return response.InternalError(c)
	}

	// Enforce per-document access. Return 404 (not 403) for ungranted users so the
	// endpoint does not confirm the existence of documents they cannot see.
	if ok, err := h.canAccessDocument(c, doc, domain.AccessView); err != nil {
		return response.InternalError(c)
	} else if !ok {
		return response.NotFound(c, "document not found")
	}

	versions, err := h.docRepo.ListVersions(c.Request().Context(), id)
	if err != nil {
		return response.InternalError(c)
	}
	// Repair available legacy PDF versions using only prefixes and one shared
	// short deadline. Metadata remains usable when an original is offline. Binary
	// routes require successful resolution and full integrity verification.
	ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
	defer cancel()
	for _, v := range versions {
		if ctx.Err() == nil {
			_ = h.resolveVersionMIME(ctx, v)
		}
		if v.VersionNumber == doc.CurrentVersion {
			doc.MimeType = v.MimeType
		}
	}

	canDownload, err := h.canAccessDocument(c, doc, domain.AccessDownload)
	if err != nil {
		return response.InternalError(c)
	}
	doc.CanDownload = canDownload
	role := middleware.GetUserRole(c)
	return response.OK(c, "Document retrieved", map[string]any{
		"permissions": map[string]bool{"can_download": canDownload, "can_manage": role == domain.RoleAdmin || role == domain.RoleCompanyMember},
		"document":    doc,
		"versions":    versions,
	})
}

// Upload handles POST /documents (multipart/form-data).
func (h *DocumentHandler) Upload(c echo.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return response.BadRequest(c, "file is required")
	}

	if file.Size > h.maxUploadSize {
		return response.TooLarge(c, fmt.Sprintf("file exceeds maximum size of %d bytes", h.maxUploadSize))
	}

	name := c.FormValue("name")
	if name == "" {
		return response.BadRequest(c, "name is required")
	}
	description := c.FormValue("description")
	categoryID := c.FormValue("category_id")
	if categoryID == "" {
		return response.BadRequest(c, "category_id is required")
	}
	tags := c.FormValue("tags")

	src, err := file.Open()
	if err != nil {
		return response.InternalError(c)
	}
	defer src.Close()

	fileData, err := io.ReadAll(src)
	if err != nil {
		return response.InternalError(c)
	}

	checksum := sha256.Sum256(fileData)
	checksumHex := hex.EncodeToString(checksum[:])
	safeFilename := sanitize.FileName(file.Filename)
	// Derive the MIME type from the actual bytes rather than trusting the
	// client-supplied Content-Type, which is later echoed back on download.
	mimeType := detectDocumentMIME(fileData)

	docID, err := generateHandlerID()
	if err != nil {
		return response.InternalError(c)
	}
	versionID, err := generateHandlerID()
	if err != nil {
		return response.InternalError(c)
	}

	userID := middleware.GetUserID(c)

	doc := &domain.Document{
		ID:             docID,
		Name:           name,
		Description:    description,
		CategoryID:     categoryID,
		UploadedBy:     userID,
		CurrentVersion: 1,
		MimeType:       mimeType,
		FileSize:       int64(len(fileData)),
		Tags:           tags,
	}

	version := &domain.DocumentVersion{
		ID:             versionID,
		DocumentID:     docID,
		VersionNumber:  1,
		FileData:       fileData,
		FileSize:       int64(len(fileData)),
		MimeType:       mimeType,
		ChecksumSHA256: checksumHex,
		ChangeNote:     "Initial upload",
		UploadedBy:     userID,
	}

	// Insert the document and its first version atomically so a failure cannot
	// leave an orphan document with no downloadable version.
	if err := h.storeVersion(c.Request().Context(), version, func() error { return h.docRepo.CreateWithVersion(c.Request().Context(), doc, version) }); err != nil {
		return storageResponseError(c, err)
	}

	h.audit.LogFromContext(c, domain.AuditDocumentUploaded, "document", docID, safeFilename, "")

	return response.Created(c, "Document uploaded", doc)
}

type updateDocumentRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	CategoryID  string `json:"category_id"`
	Tags        string `json:"tags"`
}

// Update handles PUT /documents/:id.
func (h *DocumentHandler) Update(c echo.Context) error {
	id := c.Param("id")
	var req updateDocumentRequest
	if err := c.Bind(&req); err != nil {
		return response.BadRequest(c, "invalid request body")
	}

	doc, err := h.docRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		if err == domain.ErrDocumentNotFound {
			return response.NotFound(c, "document not found")
		}
		return response.InternalError(c)
	}

	if doc.IsArchived {
		return response.NotFound(c, "document not found")
	}
	if req.Name != "" {
		doc.Name = req.Name
	}
	if req.Description != "" {
		doc.Description = req.Description
	}
	if req.CategoryID != "" {
		doc.CategoryID = req.CategoryID
	}
	if req.Tags != "" {
		doc.Tags = req.Tags
	}

	if err := h.docRepo.Update(c.Request().Context(), doc); err != nil {
		return response.InternalError(c)
	}

	h.audit.LogFromContext(c, domain.AuditDocumentUpdated, "document", id, doc.Name, "")

	return response.OK(c, "Document updated", doc)
}

// Archive handles DELETE /documents/:id, retaining only the document tombstone.
func (h *DocumentHandler) Archive(c echo.Context) error {
	id := c.Param("id")

	if err := h.docRepo.Archive(c.Request().Context(), id); err != nil {
		if err == domain.ErrDocumentNotFound {
			return response.NotFound(c, "document not found")
		}
		return response.InternalError(c)
	}

	h.audit.LogFromContext(c, domain.AuditDocumentArchived, "document", id, "", "")
	if h.content != nil {
		h.content.ScheduleCleanup()
	}

	return response.OK(c, "Document archived", nil)
}

// UploadVersion handles POST /documents/:id/versions.
func (h *DocumentHandler) UploadVersion(c echo.Context) error {
	docID := c.Param("id")

	doc, err := h.docRepo.GetByID(c.Request().Context(), docID)
	if err != nil {
		if err == domain.ErrDocumentNotFound {
			return response.NotFound(c, "document not found")
		}
		return response.InternalError(c)
	}

	if doc.IsArchived {
		return response.NotFound(c, "document not found")
	}
	file, err := c.FormFile("file")
	if err != nil {
		return response.BadRequest(c, "file is required")
	}

	if file.Size > h.maxUploadSize {
		return response.TooLarge(c, fmt.Sprintf("file exceeds maximum size of %d bytes", h.maxUploadSize))
	}

	changeNote := c.FormValue("change_note")

	src, err := file.Open()
	if err != nil {
		return response.InternalError(c)
	}
	defer src.Close()

	fileData, err := io.ReadAll(src)
	if err != nil {
		return response.InternalError(c)
	}

	checksum := sha256.Sum256(fileData)
	checksumHex := hex.EncodeToString(checksum[:])
	// Derive MIME from the actual bytes, not the client-supplied Content-Type.
	mimeType := detectDocumentMIME(fileData)

	versionID, err := generateHandlerID()
	if err != nil {
		return response.InternalError(c)
	}

	userID := middleware.GetUserID(c)

	version := &domain.DocumentVersion{
		ID:             versionID,
		DocumentID:     docID,
		FileData:       fileData,
		FileSize:       int64(len(fileData)),
		MimeType:       mimeType,
		ChecksumSHA256: checksumHex,
		ChangeNote:     changeNote,
		UploadedBy:     userID,
	}

	// Insert the version and advance the document pointer atomically. AddVersion
	// computes the new version number inside the transaction (race-safe) and
	// updates doc.CurrentVersion / MimeType / FileSize on success.
	if err := h.storeVersion(c.Request().Context(), version, func() error { return h.docRepo.AddVersion(c.Request().Context(), doc, version) }); err != nil {
		return storageResponseError(c, err)
	}

	h.audit.LogFromContext(c, domain.AuditDocumentNewVersion, "document", docID, doc.Name,
		fmt.Sprintf("version=%d", doc.CurrentVersion))

	return response.Created(c, "New version uploaded", version)
}

// DownloadVersion handles GET /documents/:id/versions/:version.
func (h *DocumentHandler) DownloadVersion(c echo.Context) error { return h.deliverFile(c, false) }
func (h *DocumentHandler) Download(c echo.Context) error        { return h.deliverFile(c, false) }

type searchRequest struct {
	Query string `json:"query"`
}

// Search handles POST /documents/search.
func (h *DocumentHandler) Search(c echo.Context) error {
	var req searchRequest
	if err := c.Bind(&req); err != nil {
		return response.BadRequest(c, "invalid request body")
	}
	if req.Query == "" {
		return response.BadRequest(c, "query is required")
	}

	return h.listDocuments(c, req.Query, "Search results")
}

func (h *DocumentHandler) setDownloadPermissions(c echo.Context, docs []*domain.Document) error {
	for _, doc := range docs {
		allowed, err := h.canAccessDocument(c, doc, domain.AccessDownload)
		if err != nil {
			return err
		}
		doc.CanDownload = allowed
	}
	return nil
}

func (h *DocumentHandler) DeleteVersion(c echo.Context) error {
	number, err := strconv.Atoi(c.Param("version"))
	if err != nil || number < 1 {
		return response.BadRequest(c, "invalid version number")
	}
	doc, err := h.docRepo.GetByID(c.Request().Context(), c.Param("id"))
	if err == domain.ErrDocumentNotFound {
		return response.NotFound(c, "document not found")
	}
	if err != nil {
		return response.InternalError(c)
	}
	if doc.IsArchived {
		return response.NotFound(c, "document not found")
	}
	if err := h.docRepo.DeleteVersion(c.Request().Context(), doc.ID, number); err != nil {
		if err == domain.ErrVersionNotFound {
			return response.NotFound(c, "version not found")
		}
		if err == domain.ErrLastVersion {
			return response.Conflict(c, err.Error())
		}
		return response.InternalError(c)
	}
	h.audit.LogFromContext(c, domain.AuditDocumentVersionDeleted, "document", doc.ID, doc.Name, fmt.Sprintf("version=%d", number))
	if h.content != nil {
		h.content.ScheduleCleanup()
	}
	return response.OK(c, "Version deleted", nil)
}
