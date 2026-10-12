package render

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/encode"
	"github.com/z0rr0/image89/internal/figure"
)

// Benchmarks of every figure with the default presets; docs/benchmarks.md
// has the results. `make bench` runs them, BENCH selects some and BENCHFLAGS
// adds flags, e.g. BENCH=Generate BENCHFLAGS='-count=10'.

// benchSizes returns the image sizes of the generation matrix.
func benchSizes() []figure.Size {
	return []figure.Size{
		{Width: 640, Height: 480},
		{Width: 480, Height: 640},
		{Width: 640, Height: 640},
		{Width: 1920, Height: 1080},
		{Width: 4096, Height: 4096},
	}
}

// generate returns an image of kind drawn by r; it fails the benchmark on an error.
func generate(b *testing.B, r *Renderer, kind figure.Kind, size figure.Size) *image.RGBA {
	b.Helper()
	img, err := r.gens[kind].Generate(b.Context(), size)
	if err != nil {
		b.Fatal(err)
	}
	return img
}

// BenchmarkGenerate draws every figure of every size without encoding.
func BenchmarkGenerate(b *testing.B) {
	r := New(config.Default())
	for _, kind := range r.Kinds() {
		for _, size := range benchSizes() {
			b.Run(string(kind)+"/"+size.String(), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					generate(b, r, kind, size)
				}
			})
		}
	}
}

// BenchmarkEncode encodes every figure of 1920×1080 with the candidate
// settings of the formats and reports the size of the encoded image.
func BenchmarkEncode(b *testing.B) {
	variants := []struct {
		name   string
		encode func(w io.Writer, img image.Image) error
	}{
		{"png-default", func(w io.Writer, img image.Image) error {
			enc := png.Encoder{CompressionLevel: png.DefaultCompression}
			return enc.Encode(w, img)
		}},
		{"png-speed", func(w io.Writer, img image.Image) error {
			enc := png.Encoder{CompressionLevel: png.BestSpeed}
			return enc.Encode(w, img)
		}},
		{"jpeg-85", func(w io.Writer, img image.Image) error {
			return jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
		}},
		{"jpeg-90", func(w io.Writer, img image.Image) error {
			return jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
		}},
	}

	r := New(config.Default())
	size := figure.Size{Width: 1920, Height: 1080}
	for _, kind := range r.Kinds() {
		img := generate(b, r, kind, size)
		for _, v := range variants {
			b.Run(string(kind)+"/"+v.name, func(b *testing.B) {
				var buf bytes.Buffer
				b.ReportAllocs()
				for b.Loop() {
					buf.Reset()
					if err := v.encode(&buf, img); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(buf.Len()), "B/img")
			})
		}
	}
}

// BenchmarkWorkers draws every figure of 1920×1080 by one goroutine and by
// GOMAXPROCS goroutines.
func BenchmarkWorkers(b *testing.B) {
	size := figure.Size{Width: 1920, Height: 1080}
	for _, workers := range []int{1, 0} {
		cfg := config.Default()
		cfg.Limits.Workers = workers
		r := New(cfg)
		name := "max"
		if workers > 0 {
			name = strconv.Itoa(workers)
		}
		for _, kind := range r.Kinds() {
			b.Run(string(kind)+"/"+name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					generate(b, r, kind, size)
				}
			})
		}
	}
}

// heavyConfig returns the default config with the slowest presets the
// default limits allow and thick lines.
func heavyConfig() *config.Config {
	cfg := config.Default()
	t, l := &cfg.Types, &cfg.Limits
	t.Mandelbrot.Iterations = l.MaxIterations
	t.Julia.Iterations = l.MaxIterations
	t.Newton.Iterations = l.MaxIterations
	t.Sierpinski.Depth = l.MaxDepth
	t.Koch.Depth = l.MaxDepth
	t.Koch.LineWidth = 0.02
	t.Lissajous.A, t.Lissajous.B = 31, 32
	t.Lissajous.Points = 65536
	t.Lissajous.LineWidth = 0.02
	return cfg
}

// BenchmarkCancel measures how long a generator of 4096×4096 with a heavy
// preset keeps working after its ctx is canceled in the middle of the work:
// ms/cancel is the mean and max-ms/cancel the largest delay. A generation
// that ends before the cancel is not counted.
func BenchmarkCancel(b *testing.B) {
	r := New(heavyConfig())
	size := figure.Size{Width: 4096, Height: 4096}
	for _, kind := range r.Kinds() {
		b.Run(string(kind), func(b *testing.B) {
			gen := r.gens[kind]
			start := time.Now()
			generate(b, r, kind, size)
			half := time.Since(start) / 2

			var total, worst time.Duration
			canceled := 0
			for b.Loop() {
				ctx, cancel := context.WithCancel(b.Context())
				var at atomic.Int64
				timer := time.AfterFunc(half, func() {
					at.Store(time.Now().UnixNano())
					cancel()
				})
				_, err := gen.Generate(ctx, size)
				end := time.Now()
				timer.Stop()
				cancel()
				if err == nil || at.Load() == 0 {
					continue
				}
				delay := end.Sub(time.Unix(0, at.Load()))
				total += delay
				worst = max(worst, delay)
				canceled++
			}
			if canceled == 0 {
				b.Fatal("every generation ended before the cancel")
			}
			ms := float64(time.Millisecond)
			b.ReportMetric(float64(total)/float64(canceled)/ms, "ms/cancel")
			b.ReportMetric(float64(worst)/ms, "max-ms/cancel")
		})
	}
}

// BenchmarkRender runs the whole pipeline for every figure of 1024×1024 in
// both formats.
func BenchmarkRender(b *testing.B) {
	r := New(config.Default())
	size := figure.Size{Width: figure.DefaultSide, Height: figure.DefaultSide}
	for _, kind := range r.Kinds() {
		for _, format := range []encode.Format{encode.PNG, encode.JPEG} {
			b.Run(string(kind)+"/"+string(format), func(b *testing.B) {
				req := Request{Kind: kind, Size: size, Format: format}
				b.ReportAllocs()
				for b.Loop() {
					if err := r.Render(b.Context(), req, discard); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
