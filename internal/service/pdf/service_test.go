package pdf

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/witfoo/due-diligence-portal/internal/domain"
)

func TestPrepareKeysAndLimits(t *testing.T) {
	cfg, _ := LoadConfig()
	cfg.CacheDir = t.TempDir()
	s, e := NewService(cfg, nil, nil)
	require.NoError(t, e)
	defer s.Close()
	data, _ := os.ReadFile("testdata/sample.pdf")
	src := Source{DocumentID: "d", DocumentName: "name", Version: 1, Checksum: fmt.Sprintf("%x", sha256.Sum256(data)), Size: int64(len(data)), MimeType: "application/pdf"}
	id := Identity{ID: "a", Email: "a@test"}
	wc := domain.WatermarkConfig{Enabled: true, TextTemplate: "{{user_email}} {{date}} {{document_name}}", Position: "center", FontSize: 12, Opacity: .2, Color: "#888888"}
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	var loads atomic.Int32
	load := func(context.Context) ([]byte, error) { loads.Add(1); return data, nil }
	a, e := s.Prepare(context.Background(), src, id, wc, now, load)
	require.NoError(t, e)
	defer a.Release()
	b, e := s.Prepare(context.Background(), src, id, wc, now, load)
	require.NoError(t, e)
	defer b.Release()
	require.Equal(t, a.Path, b.Path)
	require.Equal(t, int32(1), loads.Load())
	for _, change := range []func(*Source, *Identity, *domain.WatermarkConfig, *time.Time){func(_ *Source, id *Identity, _ *domain.WatermarkConfig, _ *time.Time) { id.ID = "b" }, func(_ *Source, _ *Identity, w *domain.WatermarkConfig, _ *time.Time) { w.Opacity = .5 }, func(src *Source, _ *Identity, _ *domain.WatermarkConfig, _ *time.Time) { src.DocumentName = "renamed" }, func(src *Source, _ *Identity, _ *domain.WatermarkConfig, _ *time.Time) { src.Version = 2 }, func(_ *Source, _ *Identity, _ *domain.WatermarkConfig, t *time.Time) { *t = t.Add(24 * time.Hour) }} {
		ss, ii, ww, nn := src, id, wc, now
		change(&ss, &ii, &ww, &nn)
		v, e := s.Prepare(context.Background(), ss, ii, ww, nn, load)
		require.NoError(t, e)
		require.NotEqual(t, a.Key, v.Key)
		v.Release()
	}
	src.Size = 101 << 20
	before := loads.Load()
	_, e = s.Prepare(context.Background(), src, id, wc, now, load)
	require.ErrorIs(t, e, ErrTooLarge)
	require.Equal(t, before, loads.Load())
}
