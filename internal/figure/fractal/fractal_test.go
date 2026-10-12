package fractal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"sync/atomic"
	"testing"
	"time"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

// generator is the interface that render declares for the generators.
type generator interface {
	Generate(ctx context.Context, size figure.Size) (*image.RGBA, error)
}

// generators returns the generators of this package by name with the default
// presets; iterations > 0 replaces the preset iterations to make tests cheaper:
// under -race -cover every iteration updates an atomic coverage counter.
func generators(workers, iterations int) map[string]generator {
	types := config.Default().Types
	if iterations > 0 {
		types.Mandelbrot.Iterations = iterations
		types.Julia.Iterations = iterations
	}
	return map[string]generator{
		"mandelbrot": NewMandelbrot(types.Mandelbrot, workers),
		"julia":      NewJulia(types.Julia, workers),
	}
}

// insideMandelbrot is a preset whose whole area lies inside the main cardioid.
func insideMandelbrot(iterations int) config.Mandelbrot {
	p := config.Default().Types.Mandelbrot
	p.Iterations = iterations
	p.Center = []float64{-0.1, 0}
	p.Width, p.Height = 0.01, 0.01
	return p
}

// countdownCtx is never done by itself, but its Err starts to report
// context.Canceled after left calls, which stops a generator at a known check
// without timing.
type countdownCtx struct {
	left atomic.Int64
}

func (*countdownCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*countdownCtx) Done() <-chan struct{}       { return nil }
func (*countdownCtx) Value(any) any               { return nil }

func (c *countdownCtx) Err() error {
	if c.left.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}

// Tests that generate images are sequential and use one worker: under
// -race -cover all goroutines update the same atomic coverage counters, and
// their contention makes the package several times slower.

func TestGenerateSizes(t *testing.T) {
	sizes := []figure.Size{{Width: 640, Height: 480}, {Width: 480, Height: 640}, {Width: 640, Height: 640}, {Width: 32, Height: 32}}
	for name, gen := range generators(1, 4) {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%s_%dx%d", name, size.Width, size.Height), func(t *testing.T) {
				img, err := gen.Generate(t.Context(), size)
				if err != nil {
					t.Fatalf("Generate error: %v", err)
				}
				if want := image.Rect(0, 0, size.Width, size.Height); img.Bounds() != want {
					t.Fatalf("bounds = %v, want %v", img.Bounds(), want)
				}
				for i := 3; i < len(img.Pix); i += 4 {
					if img.Pix[i] != 0xff {
						t.Fatalf("pixel %d has alpha %d, want opaque", i/4, img.Pix[i])
					}
				}
			})
		}
	}
}

func TestGenerateDeterministic(t *testing.T) {
	size := figure.Size{Width: 160, Height: 120}
	single, parallel := generators(1, 0), generators(4, 0)
	for name := range single {
		t.Run(name, func(t *testing.T) {
			first, err := parallel[name].Generate(t.Context(), size)
			if err != nil {
				t.Fatalf("Generate error: %v", err)
			}
			second, err := parallel[name].Generate(t.Context(), size)
			if err != nil {
				t.Fatalf("Generate error: %v", err)
			}
			one, err := single[name].Generate(t.Context(), size)
			if err != nil {
				t.Fatalf("Generate error: %v", err)
			}
			if !bytes.Equal(first.Pix, second.Pix) {
				t.Error("two calls differ")
			}
			if !bytes.Equal(first.Pix, one.Pix) {
				t.Error("1 and 4 workers differ")
			}
		})
	}
}

func TestMandelbrotSymmetry(t *testing.T) {
	for _, size := range []figure.Size{{Width: 160, Height: 120}, {Width: 161, Height: 121}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			// The default preset is centered on the real axis.
			img, err := NewMandelbrot(config.Default().Types.Mandelbrot, 1).Generate(t.Context(), size)
			if err != nil {
				t.Fatalf("Generate error: %v", err)
			}
			for y := range size.Height / 2 {
				row := img.Pix[y*img.Stride : (y+1)*img.Stride]
				mirror := img.Pix[(size.Height-1-y)*img.Stride : (size.Height-y)*img.Stride]
				if !bytes.Equal(row, mirror) {
					t.Fatalf("row %d differs from row %d", y, size.Height-1-y)
				}
			}
		})
	}
}

func TestGenerateColors(t *testing.T) {
	size := figure.Size{Width: 64, Height: 48}
	first := config.Default().Types.Mandelbrot.Palette[0].RGBA()

	inside, err := NewMandelbrot(insideMandelbrot(256), 1).Generate(t.Context(), size)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	for i := 0; i < len(inside.Pix); i += 4 {
		if inside.Pix[i] != first.R || inside.Pix[i+1] != first.G || inside.Pix[i+2] != first.B {
			t.Fatalf("inside pixel %d = %v, want the first palette color %v", i/4, inside.Pix[i:i+4], first)
		}
	}

	far := config.Default().Types.Mandelbrot
	far.Center = []float64{5, 5}
	outside, err := NewMandelbrot(far, 1).Generate(t.Context(), size)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	for i := 0; i < len(outside.Pix); i += 4 {
		if outside.Pix[i] == first.R && outside.Pix[i+1] == first.G && outside.Pix[i+2] == first.B {
			t.Fatalf("escaped pixel %d has the first palette color %v", i/4, first)
		}
	}
}

func TestGeneratorCopiesPreset(t *testing.T) {
	size := figure.Size{Width: 64, Height: 48}
	want, err := generators(1, 0)["julia"].Generate(t.Context(), size)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	p := config.Default().Types.Julia
	gen := NewJulia(p, 1)
	p.C[0], p.C[1] = 0, 0
	p.Palette[1] = config.Color{R: 0xff}
	got, err := gen.Generate(t.Context(), size)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Error("changing the preset slices after NewJulia changed the image")
	}
}

func TestGenerateErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		canceled bool
		size     figure.Size
		want     error
	}{
		{name: "canceled", canceled: true, size: figure.Size{Width: 64, Height: 48}, want: context.Canceled},
		{name: "invalid size", size: figure.Size{Width: 10, Height: 10}, want: figure.ErrInvalidSize},
	}

	for name, gen := range generators(0, 0) {
		for _, tt := range tests {
			t.Run(name+"_"+tt.name, func(t *testing.T) {
				t.Parallel()

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if tt.canceled {
					cancel()
				}
				img, err := gen.Generate(ctx, tt.size)
				if !errors.Is(err, tt.want) {
					t.Errorf("Generate error = %v, want %v", err, tt.want)
				}
				if img != nil {
					t.Error("Generate returned an image with an error")
				}
			})
		}
	}
}

// TestRowsStopInsideBlock checks the fine ctx check: a block of 4096×16 inside
// points at 4096 iterations is far larger than checkEvery, so the first check
// stops it long before the block ends.
func TestRowsStopInsideBlock(t *testing.T) {
	size := figure.Size{Width: 4096, Height: blockRows}
	m := NewMandelbrot(insideMandelbrot(4096), 1)
	img := image.NewRGBA(image.Rect(0, 0, size.Width, size.Height))
	view := newViewport(m.e.center, m.e.width, m.e.height, size)

	err := m.e.rows(&countdownCtx{}, img, view, 0, size.Height)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("rows error = %v, want %v", err, context.Canceled)
	}
	if img.Pix[3] != 0xff {
		t.Error("the first pixel is not drawn before the check")
	}
	if last := img.Pix[len(img.Pix)-1]; last != 0 {
		t.Errorf("the last pixel of the block has alpha %d, want 0: the block was not stopped", last)
	}
}

func BenchmarkMandelbrot(b *testing.B) {
	gen := NewMandelbrot(config.Default().Types.Mandelbrot, 0)
	for _, size := range []figure.Size{{Width: 640, Height: 480}, {Width: 1920, Height: 1080}} {
		b.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := gen.Generate(b.Context(), size); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkJulia(b *testing.B) {
	gen := NewJulia(config.Default().Types.Julia, 0)
	size := figure.Size{Width: 640, Height: 480}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := gen.Generate(b.Context(), size); err != nil {
			b.Fatal(err)
		}
	}
}
