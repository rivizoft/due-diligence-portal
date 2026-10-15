package pdf

import (
	"context"
	_ "embed"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
)

//go:embed assets/DejaVuSans.ttf
var fontBytes []byte
var fontOnce sync.Once
var fontError error

type textWatermarker struct{}

func NewWatermarker() (Watermarker, error) {
	fontOnce.Do(func() {
		model.ConfigPath = "disable"
		var dir string
		dir, fontError = os.MkdirTemp("", "portal-pdf-font-")
		if fontError != nil {
			return
		}
		font.UserFontDir = dir
		fontError = font.InstallFontFromBytes(dir, "DejaVuSans.ttf", fontBytes)
		if fontError == nil {
			fontError = font.LoadUserFonts()
		}
	})
	if fontError != nil {
		return nil, fontError
	}
	return textWatermarker{}, nil
}
func (textWatermarker) Apply(ctx context.Context, in, out string, c domain.WatermarkConfig, text string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: malformed PDF", ErrRender)
		}
		if err != nil {
			_ = os.Remove(out)
		}
	}()
	if err = ctx.Err(); err != nil {
		return err
	}
	color := strings.TrimPrefix(c.Color, "#")
	if len(color) != 6 {
		return ErrRender
	}
	rgb, e := strconv.ParseUint(color, 16, 32)
	if e != nil {
		return ErrRender
	}
	conf := model.NewDefaultConfiguration()
	f, e := os.Open(in)
	if e != nil {
		return e
	}
	defer f.Close()
	pdfctx, e := readWatermarkContext(f, conf)
	if e != nil {
		return fmt.Errorf("%w: %v", ErrRender, e)
	}
	if pdfctx.Encrypt != nil {
		return ErrUnsupported
	}
	dims, e := pdfctx.PageDims()
	if e != nil {
		return fmt.Errorf("%w: %v", ErrRender, e)
	}
	stamps := map[int]*model.Watermark{}
	for i, d := range dims {
		pos, rotation := "c", 0
		switch c.Position {
		case "top":
			pos = "tc"
		case "bottom":
			pos = "bc"
		case "diagonal":
			rotation = 45
		case "center":
		default:
			return ErrRender
		}
		width := font.TextWidth(text, "DejaVuSans", c.FontSize)
		scale := 1.0
		if width > 0 {
			scale = math.Min(1, (math.Min(d.Width, d.Height)-40)/width)
		}
		if scale <= 0 {
			return ErrUnsupported
		}
		desc := fmt.Sprintf("font:DejaVuSans, points:%d, scale:%.6f abs, position:%s, rotation:%d, opacity:%.6f, fillcolor:%.6f %.6f %.6f", c.FontSize, scale, pos, rotation, c.Opacity, float64(rgb>>16)/255, float64((rgb>>8)&255)/255, float64(rgb&255)/255)
		wm, e := api.TextWatermark(strings.ReplaceAll(text, "%", "%%\u200b"), desc, true, false, types.POINTS)
		if e != nil {
			return e
		}
		if pos == "tc" {
			wm.Dy = -20
		}
		if pos == "bc" {
			wm.Dy = 20
		}
		stamps[i+1] = wm
	}
	err = pdfcpu.AddWatermarksMap(pdfctx, stamps)
	if err == nil {
		var output *os.File
		output, err = os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err == nil {
			err = api.Write(pdfctx, output, conf)
			closeErr := output.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRender, err)
	}
	return ctx.Err()
}
