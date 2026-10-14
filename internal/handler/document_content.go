package handler

import (
	"context"
	"github.com/witfoo/due-diligence-portal/internal/domain"
)

// VersionContent is the content lifecycle consumed by document delivery.
type VersionContent interface {
	Store(context.Context, *domain.DocumentVersion, func() error) error
	Read(context.Context, string, int) ([]byte, error)
	Cleanup(context.Context)
}

func (h *DocumentHandler) storeVersion(ctx context.Context, v *domain.DocumentVersion, persist func() error) error {
	if h.content == nil {
		return persist()
	}
	return h.content.Store(ctx, v, persist)
}
func (h *DocumentHandler) readVersion(ctx context.Context, id string, number int) ([]byte, error) {
	if h.content != nil {
		return h.content.Read(ctx, id, number)
	}
	v, err := h.docRepo.GetVersion(ctx, id, number)
	if err != nil {
		return nil, err
	}
	return v.FileData, nil
}
