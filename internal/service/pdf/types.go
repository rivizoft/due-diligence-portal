// Package pdf prepares private PDF and raster artifacts for authorized delivery.
package pdf

import (
	"context"
	"errors"
	"time"

	"github.com/witfoo/due-diligence-portal/internal/domain"
)

var (
	ErrBusy        = errors.New("PDF preparation is busy")
	ErrTimeout     = errors.New("PDF preparation timed out")
	ErrTooLarge    = errors.New("PDF exceeds preparation size limit")
	ErrUnsupported = errors.New("unsupported PDF")
	ErrRender      = errors.New("PDF could not be prepared")
)

type Identity struct{ ID, Email, Name string }
type Source struct {
	DocumentID, DocumentName string
	Version                  int
	Checksum                 string
	Size                     int64
	MimeType                 string
}
type Artifact struct {
	Path    string
	Size    int64
	Key     string
	Release func()
	retain  func() Artifact
}
type PageInfo struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type PreviewInfo struct {
	Pages []PageInfo `json:"pages"`
}
type Watermarker interface {
	Apply(context.Context, string, string, domain.WatermarkConfig, string) error
}
type Rasterizer interface {
	Info(context.Context, string) (PreviewInfo, error)
	Page(context.Context, string, string, int) error
}
type Config struct {
	Workers, QueueSize        int
	CacheBytes, MaxInputBytes int64
	TTL, Timeout              time.Duration
	CacheDir                  string
}
