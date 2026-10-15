package pdf

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/witfoo/due-diligence-portal/internal/domain"
)

func BenchmarkDelivery(b *testing.B) {
	file := os.Getenv("DD_PDF_BENCH_FILE")
	if file == "" {
		file = "testdata/sample.pdf"
	}
	data, err := os.ReadFile(file)
	if err != nil {
		b.Fatal(err)
	}
	source := Source{DocumentID: "bench", DocumentName: "Report.pdf", Version: 1, Checksum: fmt.Sprintf("%x", sha256.Sum256(data)), Size: int64(len(data)), MimeType: "application/pdf"}
	cfg, _ := LoadConfig()
	cfg.CacheDir = b.TempDir()
	cfg.Timeout = 60 * time.Second
	s, err := NewService(cfg, nil, NewPoppler())
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	id := Identity{ID: "bench", Email: "bench@example.com", Name: "Benchmark"}
	wm := domain.WatermarkConfig{Enabled: true, TextTemplate: "{{user_email}} {{date}}", Position: "diagonal", Opacity: .15, FontSize: 12, Color: "#888888"}
	at := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	load := func(context.Context) ([]byte, error) { return data, nil }
	a, err := s.Prepare(context.Background(), source, id, wm, at, load)
	if err != nil {
		b.Fatal(err)
	}
	defer a.Release()
	b.Run("ColdPDF", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			src := source
			src.Version = i + 100
			v, e := s.Prepare(context.Background(), src, id, wm, at, load)
			if e != nil {
				b.Fatal(e)
			}
			v.Release()
		}
	})
	b.Run("WarmPDF", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			v, e := s.Prepare(context.Background(), source, id, wm, at, load)
			if e != nil {
				b.Fatal(e)
			}
			v.Release()
		}
	})
	b.Run("ColdFirstPage", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			src := source
			src.Version = i + 10000
			v, e := s.Prepare(context.Background(), src, id, wm, at, load)
			if e != nil {
				b.Fatal(e)
			}
			p, e := s.Page(context.Background(), v, 1)
			if e != nil {
				b.Fatal(e)
			}
			p.Release()
			v.Release()
		}
	})
	p, err := s.Page(context.Background(), a, 1)
	if err != nil {
		b.Fatal(err)
	}
	p.Release()
	b.Run("WarmPage", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			p, e := s.Page(context.Background(), a, 1)
			if e != nil {
				b.Fatal(e)
			}
			p.Release()
		}
	})
	b.Run("ConcurrentSameKey", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				v, e := s.Prepare(context.Background(), source, id, wm, at, load)
				if e != nil {
					b.Error(e)
					return
				}
				v.Release()
			}
		})
	})
	b.Run("ConcurrentDistinctUsers", func(b *testing.B) {
		var count atomic.Int64
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				user := id
				user.ID = fmt.Sprint(count.Add(1))
				v, e := s.Prepare(context.Background(), source, user, wm, at, load)
				if e != nil {
					b.Error(e)
					return
				}
				v.Release()
			}
		})
	})
}
