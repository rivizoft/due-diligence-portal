package pdf

import (
	"context"
	"encoding/json"
	"os"
)

func (s *Service) Info(ctx context.Context, input Artifact) (PreviewInfo, error) {
	if s.raster == nil {
		return PreviewInfo{}, ErrUnsupported
	}
	held := input.retain()
	key := keyFor(struct{ PDF, Kind, Renderer string }{input.Key, "info", rasterVersion})
	a, err := s.cache.do(ctx, key, func(ctx context.Context, out string) error {
		info, e := s.raster.Info(ctx, held.Path)
		if e != nil {
			return e
		}
		b, e := json.Marshal(info)
		if e != nil {
			return e
		}
		return os.WriteFile(out, b, 0600)
	}, held.Release)
	if err != nil {
		return PreviewInfo{}, err
	}
	defer a.Release()
	data, err := os.ReadFile(a.Path)
	if err != nil {
		return PreviewInfo{}, err
	}
	var info PreviewInfo
	err = json.Unmarshal(data, &info)
	return info, err
}
func (s *Service) Page(ctx context.Context, input Artifact, page int) (Artifact, error) {
	info, err := s.Info(ctx, input)
	if err != nil {
		return Artifact{}, err
	}
	if page < 1 || page > len(info.Pages) {
		return Artifact{}, ErrUnsupported
	}
	held := input.retain()
	key := keyFor(struct {
		PDF, Renderer string
		Page          int
	}{input.Key, rasterVersion, page})
	return s.cache.do(ctx, key, func(ctx context.Context, out string) error { return s.raster.Page(ctx, held.Path, out, page) }, held.Release)
}
