package fractal

import (
	"bytes"
	"testing"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

func TestInMainBulbs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		c    complex128
		want bool
	}{
		{c: 0, want: true},
		{c: -0.1 + 0.1i, want: true},
		{c: 0.5i, want: true},
		{c: 0.25, want: true},  // the cusp of the cardioid
		{c: -0.75, want: true}, // the point where the cardioid touches the disk
		{c: -1, want: true},    // the center of the disk of period 2
		{c: -1.25, want: true}, // the left end of the disk
		{c: -1 + 0.24i, want: true},
		{c: 0.26},
		{c: -1.26},
		{c: -1 + 0.26i},
		{c: -0.75 + 0.1i}, // between the cardioid and the disk
		{c: 1i},
		{c: -2},
	}
	for _, tt := range tests {
		if got := inMainBulbs(tt.c); got != tt.want {
			t.Errorf("inMainBulbs(%v) = %v, want %v", tt.c, got, tt.want)
		}
	}
}

// TestInMainBulbsDoNotEscape checks on a grid over the set that no point
// skipped by inMainBulbs escapes within many iterations.
func TestInMainBulbsDoNotEscape(t *testing.T) {
	t.Parallel()

	const steps, maxIter = 96, 4096
	inside := 0
	for i := range steps {
		for j := range steps {
			c := complex(-2+2.5*float64(i)/steps, -1.25+2.5*float64(j)/steps)
			if !inMainBulbs(c) {
				continue
			}
			inside++
			if n, _ := escapeMandelbrot(c, maxIter); n != maxIter {
				t.Errorf("point %v of the main bulbs escapes after %d iterations", c, n)
			}
		}
	}
	if inside == 0 {
		t.Fatal("no grid point in the main bulbs")
	}
}

// TestMandelbrotBulbsSameImage checks that skipping the main bulbs keeps the
// image byte for byte: the default view, and small views around the edges of
// the bulbs with many iterations, where slow orbits lie next to them. It
// generates images, so it runs sequentially (see AGENTS.md).
func TestMandelbrotBulbsSameImage(t *testing.T) {
	view := func(iterations int, center complex128, side float64) config.Mandelbrot {
		p := config.Default().Types.Mandelbrot
		p.Iterations = iterations
		p.Center = []float64{real(center), imag(center)}
		p.Width, p.Height = side, side
		return p
	}
	tests := []struct {
		name   string
		preset config.Mandelbrot
		size   figure.Size
	}{
		{name: "default even", preset: config.Default().Types.Mandelbrot, size: figure.Size{Width: 160, Height: 120}},
		{name: "default odd", preset: config.Default().Types.Mandelbrot, size: figure.Size{Width: 161, Height: 121}},
		{name: "cusp", preset: view(4096, 0.25, 0.01), size: figure.Size{Width: 32, Height: 32}},
		{name: "neck", preset: view(4096, -0.75+0.05i, 0.1), size: figure.Size{Width: 32, Height: 32}},
		{name: "disk edge", preset: view(4096, -1.25, 0.01), size: figure.Size{Width: 32, Height: 32}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.preset
			maxIter := p.Iterations
			// The plane of NewMandelbrot without skipping the main bulbs.
			plain := escapeTime(complex(p.Center[0], p.Center[1]), p.Width, p.Height, maxIter, p.Palette, 1,
				func(c complex128) (int, complex128) { return escapeMandelbrot(c, maxIter) })
			want, err := plain.generate(t.Context(), tt.size)
			if err != nil {
				t.Fatalf("plain generate: %v", err)
			}
			got, err := NewMandelbrot(p, 1).Generate(t.Context(), tt.size)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Error("skipping the main bulbs changed the image")
			}
		})
	}
}
