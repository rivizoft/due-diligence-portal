package handler

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
)

// PDF readers accept a header within the first 1024 bytes. Go's HTTP sniffer
// recognizes it only at offset zero, misclassifying PDFs with a BOM or preamble.
func detectDocumentMIME(data []byte) string {
	typ := http.DetectContentType(data)
	if !genericDocumentMIME(typ) {
		return typ
	}
	header := data[:min(len(data), 1024)]
	if i := bytes.Index(header, []byte("%PDF-")); i >= 0 && i+8 <= len(header) {
		v := header[i+5 : i+8]
		if (v[0] == '1' && v[1] == '.' && v[2] >= '0' && v[2] <= '7') || bytes.Equal(v, []byte("2.0")) {
			return "application/pdf"
		}
	}
	return typ
}

func genericDocumentMIME(typ string) bool {
	return typ == "" || typ == "application/octet-stream" || typ == "application/octect-stream" || typ == "text/plain" || strings.HasPrefix(typ, "text/plain;")
}

// Repair old generic MIME metadata only after the caller has checked access.
// The stored original and its checksum are unchanged; S3 response headers and
// user-supplied names/types are never used to enable PDF delivery.
func (h *DocumentHandler) resolveVersionMIME(ctx context.Context, v *domain.DocumentVersion) error {
	if !genericDocumentMIME(v.MimeType) {
		return nil
	}
	var data []byte
	var err error
	if h.content != nil {
		data, err = h.content.ReadPrefix(ctx, v.DocumentID, v.VersionNumber)
	} else {
		data, err = h.docRepo.GetVersionPrefix(ctx, v.DocumentID, v.VersionNumber)
	}
	if err != nil {
		return err
	}
	if detectDocumentMIME(data) != "application/pdf" {
		return nil
	}
	if err := h.docRepo.UpdateVersionMIME(ctx, v.DocumentID, v.VersionNumber, "application/pdf"); err != nil {
		return err
	}
	v.MimeType = "application/pdf"
	return nil
}
