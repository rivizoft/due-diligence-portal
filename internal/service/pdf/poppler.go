package pdf

import (
	"context"
	"fmt"
	"image/png"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const rasterVersion = "poppler-png-1600-v1"

type poppler struct{}

func NewPoppler() Rasterizer { return poppler{} }

// limitedOutput discards surplus bytes without unbounded subprocess output buffering.
type limitedOutput struct {
	data     []byte
	limit    int
	overflow bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := w.limit - len(w.data)
	if n > remaining {
		w.overflow = true
		p = p[:remaining]
	}
	w.data = append(w.data, p...)
	return n, nil
}
func runPoppler(ctx context.Context, name string, args ...string) ([]byte, error) {
	// prlimit execs the child; CommandContext kills that process on cancellation.
	all := []string{"--as=536870912", "--cpu=60", "--fsize=67108864", "--", name}
	all = append(all, args...)
	cmd := exec.CommandContext(ctx, "prlimit", all...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out := &limitedOutput{limit: 2 << 20}
	stderr := &limitedOutput{limit: 8192}
	cmd.Stdout = out
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %s failed", ErrRender, name)
	}
	if out.overflow {
		return nil, ErrUnsupported
	}
	return out.data, nil
}

var pageCountPattern = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)
var pageSizePattern = regexp.MustCompile(`(?m)^Page\s+(\d+)\s+size:\s+([\d.]+) x ([\d.]+) pts`)
var pageRotationPattern = regexp.MustCompile(`(?m)^Page\s+(\d+)\s+rot:\s+(-?\d+)`)

func (poppler) Info(ctx context.Context, in string) (PreviewInfo, error) {
	data, err := runPoppler(ctx, "pdfinfo", in)
	if err != nil {
		return PreviewInfo{}, err
	}
	text := string(data)
	if regexp.MustCompile(`(?m)^Encrypted:\s+yes`).MatchString(text) {
		return PreviewInfo{}, ErrUnsupported
	}
	m := pageCountPattern.FindStringSubmatch(text)
	if len(m) != 2 {
		return PreviewInfo{}, ErrRender
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 || n > 10000 {
		return PreviewInfo{}, ErrUnsupported
	}
	data, err = runPoppler(ctx, "pdfinfo", "-f", "1", "-l", strconv.Itoa(n), in)
	if err != nil {
		return PreviewInfo{}, err
	}
	pages := make([]PageInfo, n)
	matches := pageSizePattern.FindAllStringSubmatch(string(data), -1)
	if len(matches) != n {
		return PreviewInfo{}, ErrRender
	}
	for _, m := range matches {
		i, _ := strconv.Atoi(m[1])
		if i < 1 || i > n {
			return PreviewInfo{}, ErrRender
		}
		w, e := strconv.ParseFloat(m[2], 64)
		if e != nil {
			return PreviewInfo{}, ErrRender
		}
		h, e := strconv.ParseFloat(m[3], 64)
		if e != nil || w <= 0 || h <= 0 || w > 14400 || h > 14400 || math.IsNaN(w) || math.IsNaN(h) {
			return PreviewInfo{}, ErrUnsupported
		}
		pages[i-1] = PageInfo{w, h}
	}
	for _, m := range pageRotationPattern.FindAllStringSubmatch(string(data), -1) {
		i, _ := strconv.Atoi(m[1])
		r, _ := strconv.Atoi(m[2])
		if i < 1 || i > n {
			return PreviewInfo{}, ErrRender
		}
		if r%180 != 0 {
			pages[i-1].Width, pages[i-1].Height = pages[i-1].Height, pages[i-1].Width
		}
	}
	return PreviewInfo{Pages: pages}, nil
}
func (poppler) Page(ctx context.Context, in, out string, page int) (err error) {
	if page < 1 {
		return ErrUnsupported
	}
	defer func() {
		_ = os.Remove(out + ".png")
		if err != nil {
			_ = os.Remove(out)
		}
	}()
	_, err = runPoppler(ctx, "pdftoppm", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-singlefile", "-scale-to", "1600", "-png", in, out)
	if err != nil {
		return err
	}
	f, err := os.Open(out + ".png")
	if err != nil {
		return ErrRender
	}
	cfg, err := png.DecodeConfig(f)
	_ = f.Close()
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 1600 || cfg.Height > 1600 || cfg.Width*cfg.Height > 4000000 {
		return ErrUnsupported
	}
	if err = os.Chmod(out+".png", 0600); err != nil {
		return err
	}
	return os.Rename(out+".png", out)
}

// CheckRuntime is called during startup, so deployment cannot silently lack raster tools.
func CheckRuntime() error {
	for _, name := range strings.Fields("pdfinfo pdftoppm prlimit") {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("PDF preview requires %s: %w", name, err)
		}
	}
	return nil
}
