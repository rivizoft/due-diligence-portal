package pdf

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
)

func TestRenderText(t *testing.T) {
	at := time.Date(2026, 10, 14, 1, 0, 0, 0, time.FixedZone("east", 4*3600))
	got, err := RenderText("{{user_email}} {{user_email}} / {{user_name}} / {{document_name}} / {{date}}", Identity{ID: "u", Email: "a@example.com", Name: "Иван {{date}}"}, "Doc, (draft)", at)
	require.NoError(t, err)
	require.Equal(t, "a@example.com a@example.com / Иван {{date}} / Doc, (draft) / 2026-10-13", got)
	for _, v := range []string{"{{unknown}}", "{{date}", "{{}}"} {
		_, err = RenderText(v, Identity{}, "", at)
		require.Error(t, err, v)
	}
}
func TestConfig(t *testing.T) {
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, 2, cfg.Workers)
	require.Equal(t, 8, cfg.QueueSize)
	require.Equal(t, int64(512<<20), cfg.CacheBytes)
	require.Equal(t, 15*time.Minute, cfg.TTL)
	require.Equal(t, 60*time.Second, cfg.Timeout)
	require.Equal(t, int64(100<<20), cfg.MaxInputBytes)
	t.Setenv("DD_PDF_WORKERS", "0")
	_, err = LoadConfig()
	require.Error(t, err)
}
func TestWatermarker(t *testing.T) {
	in := "testdata/sample.pdf"
	before, err := os.ReadFile(in)
	require.NoError(t, err)
	w, err := NewWatermarker()
	require.NoError(t, err)
	for _, pos := range []string{"diagonal", "top", "bottom", "center"} {
		t.Run(pos, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.pdf")
			cfg := domain.WatermarkConfig{Enabled: true, Position: pos, Opacity: .35, FontSize: 18, Color: "#888888"}
			require.NoError(t, w.Apply(context.Background(), in, out, cfg, "Иван — investor@example.com (private), text"))
			data, err := os.ReadFile(out)
			require.NoError(t, err)
			require.NotEqual(t, sha256.Sum256(before), sha256.Sum256(data))
			txt, err := exec.Command("pdftotext", "-raw", out, "-").Output()
			require.NoError(t, err)
			require.Contains(t, strings.Join(strings.Fields(string(txt)), " "), "Original searchable text page one")
			require.Contains(t, strings.Join(strings.Fields(string(txt)), " "), "Original searchable text page two")
			require.Equal(t, 2, strings.Count(strings.Join(strings.Fields(string(txt)), ""), "investor@example.com"))
			require.Contains(t, strings.Join(strings.Fields(string(txt)), ""), "Иван")
		})
	}
	after, _ := os.ReadFile(in)
	require.Equal(t, before, after)
	for _, in := range []string{"testdata/encrypted.pdf", filepath.Join(t.TempDir(), "bad.pdf")} {
		if strings.HasSuffix(in, "bad.pdf") {
			require.NoError(t, os.WriteFile(in, []byte("not pdf"), 0600))
		}
		out := filepath.Join(t.TempDir(), "out.pdf")
		require.Error(t, w.Apply(context.Background(), in, out, domain.WatermarkConfig{Position: "center", Opacity: .2, FontSize: 12, Color: "#888888"}, "private"))
		_, err = os.Stat(out)
		require.True(t, os.IsNotExist(err))
	}
}

func TestWatermarkLiteralPercent(t *testing.T) {
	w, e := NewWatermarker()
	require.NoError(t, e)
	out := filepath.Join(t.TempDir(), "out.pdf")
	require.NoError(t, w.Apply(context.Background(), "testdata/sample.pdf", out, domain.WatermarkConfig{Position: "center", FontSize: 12, Opacity: .2, Color: "#888888"}, "100% private %p %%"))
	txt, e := exec.Command("pdftotext", "-raw", out, "-").Output()
	require.NoError(t, e)
	require.Contains(t, strings.ReplaceAll(string(txt), "\u200b", ""), "100% private %p %%")
}
