package pdf

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/witfoo/due-diligence-portal/internal/domain"
	"os"
	"time"
)

const rendererVersion = "pdfcpu-0.11.1-dejavu-v1"

type Service struct {
	cfg       Config
	cache     *Cache
	watermark Watermarker
	raster    Rasterizer
}

func NewService(cfg Config, wm Watermarker, raster Rasterizer) (*Service, error) {
	if wm == nil {
		var err error
		wm, err = NewWatermarker()
		if err != nil {
			return nil, err
		}
	}
	cache, err := NewCache(cfg)
	if err != nil {
		return nil, err
	}
	return &Service{cfg: cfg, cache: cache, watermark: wm, raster: raster}, nil
}
func (s *Service) Close() error { return s.cache.Close() }
func keyFor(v any) string       { b, _ := json.Marshal(v); return fmt.Sprintf("%x", sha256.Sum256(b)) }
func (s *Service) Prepare(ctx context.Context, source Source, id Identity, c domain.WatermarkConfig, at time.Time, load func(context.Context) ([]byte, error)) (Artifact, error) {
	if source.MimeType != "application/pdf" {
		return Artifact{}, ErrUnsupported
	}
	if source.Size > s.cfg.MaxInputBytes || source.Size < 0 {
		return Artifact{}, ErrTooLarge
	}
	text := ""
	var err error
	if c.Enabled {
		text, err = RenderText(c.TextTemplate, id, source.DocumentName, at)
		if err != nil {
			return Artifact{}, fmt.Errorf("%w: template", ErrRender)
		}
	}
	key := keyFor(struct {
		Source                Source
		Identity              string
		Enabled               bool
		Template, Position    string
		Opacity               float64
		FontSize              int
		Color, Text, Renderer string
	}{source, id.ID, c.Enabled, c.TextTemplate, c.Position, c.Opacity, c.FontSize, c.Color, text, rendererVersion})
	return s.cache.Do(ctx, key, func(ctx context.Context, out string) error {
		data, err := load(ctx)
		if err != nil {
			return err
		}
		if int64(len(data)) > s.cfg.MaxInputBytes {
			return ErrTooLarge
		}
		if source.Checksum != "" && fmt.Sprintf("%x", sha256.Sum256(data)) != source.Checksum {
			return ErrRender
		}
		if !c.Enabled {
			return os.WriteFile(out, data, 0600)
		}
		input, err := os.CreateTemp(s.cache.dir, "input-")
		if err != nil {
			return err
		}
		name := input.Name()
		defer os.Remove(name)
		_, err = input.Write(data)
		closeErr := input.Close()
		data = nil
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return s.watermark.Apply(ctx, name, out, c, text)
	})
}

func (s *Service) Timeout() time.Duration { return s.cfg.Timeout }
