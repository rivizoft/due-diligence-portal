package pdf

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
)

func TestPopplerInfoAndPage(t *testing.T) {
	r := NewPoppler()
	info, e := r.Info(context.Background(), "testdata/sample.pdf")
	require.NoError(t, e)
	require.Len(t, info.Pages, 2)
	require.Greater(t, info.Pages[0].Height, info.Pages[0].Width)
	require.Greater(t, info.Pages[1].Height, info.Pages[1].Width)
	out := filepath.Join(t.TempDir(), "page")
	require.NoError(t, r.Page(context.Background(), "testdata/sample.pdf", out, 1))
	f, e := os.Open(out)
	require.NoError(t, e)
	defer f.Close()
	img, e := png.DecodeConfig(f)
	require.NoError(t, e)
	require.LessOrEqual(t, img.Width, 1600)
	require.LessOrEqual(t, img.Height, 1600)
	require.LessOrEqual(t, img.Width*img.Height, 4000000)
	require.Error(t, r.Page(context.Background(), "testdata/sample.pdf", out, 0))
	_, e = r.Info(context.Background(), "testdata/encrypted.pdf")
	require.Error(t, e)
}
func TestRasterLazyAndCache(t *testing.T) {
	cfg, _ := LoadConfig()
	cfg.CacheDir = t.TempDir()
	s, e := NewService(cfg, nil, NewPoppler())
	require.NoError(t, e)
	defer s.Close()
	data, _ := os.ReadFile("testdata/sample.pdf")
	a, e := s.Prepare(context.Background(), Source{DocumentID: "d", Version: 1, MimeType: "application/pdf", Size: int64(len(data))}, Identity{ID: "u"}, domain.WatermarkConfig{Enabled: true, TextTemplate: "PRIVATE {{user_email}}", Position: "center", Opacity: .8, FontSize: 30, Color: "#ff0000"}, time.Now(), func(context.Context) ([]byte, error) { return data, nil })
	require.NoError(t, e)
	defer a.Release()
	info, e := s.Info(context.Background(), a)
	require.NoError(t, e)
	require.Len(t, info.Pages, 2)
	p, e := s.Page(context.Background(), a, 1)
	require.NoError(t, e)
	defer p.Release()
	again, e := s.Page(context.Background(), a, 1)
	require.NoError(t, e)
	defer again.Release()
	require.Equal(t, p.Path, again.Path)
	_, e = s.Page(context.Background(), a, 3)
	require.ErrorIs(t, e, ErrUnsupported)
	imgfile, e := os.Open(p.Path)
	require.NoError(t, e)
	defer imgfile.Close()
	img, e := png.Decode(imgfile)
	require.NoError(t, e)
	red := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > 40000 && g < 30000 && b < 30000 {
				red++
			}
		}
	}
	require.Greater(t, red, 100, "watermark pixels must be baked into PNG")
}
func TestPopplerDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := NewPoppler().Info(ctx, "testdata/sample.pdf")
	require.ErrorIs(t, e, context.Canceled)
}

func TestPopplerKillsTimedOutProcess(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pdfinfo"), []byte("#!/bin/sh\nexec /bin/sleep 20\n"), 0700))
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := NewPoppler().Info(ctx, "testdata/sample.pdf")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), time.Second)
}

// Run this test in the final image too: unembedded Helvetica requires a runtime
// replacement font independently of the embedded watermark font.
func TestPopplerOriginalTextWithAndWithoutWatermark(t *testing.T) {
	cfg, _ := LoadConfig()
	cfg.CacheDir = t.TempDir()
	s, err := NewService(cfg, nil, NewPoppler())
	require.NoError(t, err)
	defer s.Close()
	data, err := os.ReadFile("testdata/sample.pdf")
	require.NoError(t, err)
	for _, enabled := range []bool{false, true} {
		src := Source{DocumentID: "source-text", Version: 1, Size: int64(len(data)), MimeType: "application/pdf"}
		wm := domain.WatermarkConfig{Enabled: enabled, TextTemplate: "PRIVATE", Position: "center", Opacity: .8, FontSize: 30, Color: "#ff0000"}
		a, err := s.Prepare(context.Background(), src, Identity{ID: "text-check"}, wm, time.Now(), func(context.Context) ([]byte, error) { return data, nil })
		require.NoError(t, err)
		p, err := s.Page(context.Background(), a, 1)
		require.NoError(t, err)
		f, err := os.Open(p.Path)
		require.NoError(t, err)
		img, err := png.Decode(f)
		require.NoError(t, err)
		_ = f.Close()
		dark := 0
		red := 0
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if y < img.Bounds().Dy()/3 && r < 30000 && g < 30000 && b < 30000 {
					dark++
				}
				if r > 40000 && g < 30000 && b < 30000 {
					red++
				}
			}
		}
		require.Greater(t, dark, 300, "source text must remain visible with watermark enabled=%v", enabled)
		if enabled {
			require.Greater(t, red, 100)
		} else {
			require.Zero(t, red)
		}
		p.Release()
		a.Release()
	}
}
