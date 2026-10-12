package geometry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

// generator is the interface that render declares for the generators.
type generator interface {
	Generate(ctx context.Context, size figure.Size) (*image.RGBA, error)
}

// generators returns the generators of this package by name with the default
// presets and their backgrounds.
func generators(workers int) map[string]struct {
	gen        generator
	background color.RGBA
} {
	types := config.Default().Types
	return map[string]struct {
		gen        generator
		background color.RGBA
	}{
		"sierpinski": {NewSierpinski(types.Sierpinski, workers), types.Sierpinski.Background.RGBA()},
		"koch":       {NewKoch(types.Koch, workers), types.Koch.Background.RGBA()},
		"lissajous":  {NewLissajous(types.Lissajous, workers), types.Lissajous.Background.RGBA()},
	}
}

func TestGenerateSizes(t *testing.T) {
	t.Parallel()

	sizes := []figure.Size{{Width: 640, Height: 480}, {Width: 480, Height: 640}, {Width: 640, Height: 640}, {Width: 32, Height: 32}}
	for name, g := range generators(1) {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%s_%dx%d", name, size.Width, size.Height), func(t *testing.T) {
				t.Parallel()

				img, err := g.gen.Generate(t.Context(), size)
				if err != nil {
					t.Fatalf("Generate error: %v", err)
				}
				checkImage(t, img, size, g.background)
			})
		}
	}
}

// checkImage checks that img has the size, is opaque, has the background in
// the corners, which the padding keeps free, and has a figure.
func checkImage(t *testing.T, img *image.RGBA, size figure.Size, background color.RGBA) {
	t.Helper()
	if want := image.Rect(0, 0, size.Width, size.Height); img.Bounds() != want {
		t.Fatalf("bounds = %v, want %v", img.Bounds(), want)
	}
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			t.Fatalf("pixel %d has alpha %d, want opaque", i/4, img.Pix[i])
		}
	}
	for _, p := range []image.Point{{}, {X: size.Width - 1}, {Y: size.Height - 1}, {X: size.Width - 1, Y: size.Height - 1}} {
		if c := img.RGBAAt(p.X, p.Y); c != background {
			t.Errorf("corner %v = %v, want the background %v", p, c, background)
		}
	}
	if coverageOf(img, background) == 0 {
		t.Error("the figure is not drawn")
	}
}

// coverageOf returns the number of pixels that differ from the background.
func coverageOf(img *image.RGBA, background color.RGBA) int {
	n := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] != background.R || img.Pix[i+1] != background.G || img.Pix[i+2] != background.B {
			n++
		}
	}
	return n
}

func TestGenerateDeterministic(t *testing.T) {
	t.Parallel()

	size := figure.Size{Width: 320, Height: 240}
	single, parallel := generators(1), generators(4)
	for name := range single {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			first, err := parallel[name].gen.Generate(t.Context(), size)
			if err != nil {
				t.Fatalf("Generate error: %v", err)
			}
			second, err := parallel[name].gen.Generate(t.Context(), size)
			if err != nil {
				t.Fatalf("Generate error: %v", err)
			}
			one, err := single[name].gen.Generate(t.Context(), size)
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

func TestGenerateErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  func(t *testing.T) context.Context
		size figure.Size
		want error
	}{
		{
			name: "canceled",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			size: figure.Size{Width: 640, Height: 480},
			want: context.Canceled,
		},
		{
			// The checks before any work pass, the next one stops the generator.
			name: "canceled while drawing",
			ctx:  func(*testing.T) context.Context { return newCountdown(1) },
			size: figure.Size{Width: 640, Height: 480},
			want: context.Canceled,
		},
		{
			name: "invalid size",
			ctx:  func(t *testing.T) context.Context { t.Helper(); return t.Context() },
			size: figure.Size{Width: 10, Height: 10},
			want: figure.ErrInvalidSize,
		},
	}

	for name, g := range generators(0) {
		for _, tt := range tests {
			t.Run(name+"_"+tt.name, func(t *testing.T) {
				t.Parallel()

				img, err := g.gen.Generate(tt.ctx(t), tt.size)
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

// TestPaintCanceled checks that a request canceled while its geometry was
// built does not allocate the image.
func TestPaintCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	l := look{workers: 1}
	size := figure.Size{Width: 4096, Height: 4096}
	var (
		img *image.RGBA
		err error
	)
	allocs := testing.AllocsPerRun(1, func() {
		img, err = l.paint(ctx, size, nil)
	})
	if !errors.Is(err, context.Canceled) || img != nil {
		t.Errorf("paint = %v, %v; want no image and %v", img != nil, err, context.Canceled)
	}
	if allocs != 0 {
		t.Errorf("paint with a canceled ctx made %v allocations, want none", allocs)
	}

	// The check of paint passes, the one of Fill stops it.
	img, err = l.paint(newCountdown(1), figure.Size{Width: 64, Height: 48}, nil)
	if !errors.Is(err, context.Canceled) || img != nil {
		t.Errorf("paint canceled in Fill = %v, %v; want no image and %v", img != nil, err, context.Canceled)
	}
}

func BenchmarkGenerate(b *testing.B) {
	size := figure.Size{Width: 1024, Height: 1024}
	for name, g := range generators(0) {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := g.gen.Generate(b.Context(), size); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
