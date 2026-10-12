package fractal

import (
	"bytes"
	"math"
	"math/cmplx"
	"testing"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

// root returns the root exp(2πik/degree) of zⁿ − 1.
func root(k, degree int) complex128 {
	return cmplx.Rect(1, 2*math.Pi*float64(k)/float64(degree))
}

func TestNewtonRoot(t *testing.T) {
	t.Parallel()

	const (
		eps2    = 1e-12
		maxIter = 64
	)
	tests := []struct {
		name    string
		z       complex128
		degree  int
		maxIter int
		wantK   int
		maxN    int // upper bound of the steps; for k = −1 the exact count
	}{
		{name: "root 0", z: root(0, 3), degree: 3, wantK: 0, maxN: 1},
		{name: "root 1", z: root(1, 3), degree: 3, wantK: 1, maxN: 1},
		{name: "root 2", z: root(2, 3), degree: 3, wantK: 2, maxN: 1},
		{name: "near root", z: 1.1, degree: 3, wantK: 0, maxN: 6},
		{name: "far on the real axis", z: 10, degree: 3, wantK: 0, maxN: maxIter - 1},
		{name: "far up", z: 10i, degree: 3, wantK: 1, maxN: maxIter - 1},
		{name: "far down", z: -10i, degree: 3, wantK: 2, maxN: maxIter - 1},
		{name: "zero derivative", z: 0, degree: 3, wantK: -1, maxN: 0},
		{name: "too few steps", z: 10, degree: 3, maxIter: 1, wantK: -1, maxN: 1},
		{name: "degree 5 root 3", z: root(3, 5), degree: 5, wantK: 3, maxN: 1},
		{name: "degree 8 root 7", z: root(7, 8), degree: 8, wantK: 7, maxN: 1},
		{name: "degree 8 near root 4", z: -1.05 + 0.01i, degree: 8, wantK: 4, maxN: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			limit := maxIter
			if tt.maxIter > 0 {
				limit = tt.maxIter
			}
			k, n := newtonRoot(tt.z, tt.degree, eps2, limit)
			if k != tt.wantK {
				t.Errorf("newtonRoot(%v, %d) root = %d, want %d", tt.z, tt.degree, k, tt.wantK)
			}
			if tt.wantK < 0 && n != tt.maxN || n > tt.maxN {
				t.Errorf("newtonRoot(%v, %d) steps = %d, want <= %d (exact for no root)", tt.z, tt.degree, n, tt.maxN)
			}
		})
	}
}

// TestNewtonDegreeFive checks the colors of a degree 5 image: the pixel at
// every root has the color of that root, and the background is drawn exactly
// where Newton's method finds no root.
func TestNewtonDegreeFive(t *testing.T) {
	p := config.Default().Types.Newton
	p.Degree = 5
	size := figure.Size{Width: 256, Height: 256}
	img, err := NewNewton(p, 1).Generate(t.Context(), size)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	view := newViewport(0, p.Width, p.Height, size)
	background := p.Background.RGBA()
	eps2 := p.Epsilon * p.Epsilon
	for y := range size.Height {
		for x := range size.Width {
			c := img.RGBAAt(x, y)
			k, _ := newtonRoot(view.At(x, y), p.Degree, eps2, p.Iterations)
			if (c == background) != (k < 0) {
				t.Fatalf("pixel (%d, %d) = %v converges to root %d: background only without a root", x, y, c, k)
			}
		}
	}

	seen := make(map[[3]uint8]int, p.Degree)
	for k := range p.Degree {
		r := root(k, p.Degree)
		x := int(math.Round(real(r)/view.step + view.halfWidth - 0.5))
		y := int(math.Round(view.halfHeight - imag(r)/view.step - 0.5))
		c := img.RGBAAt(x, y)
		if got := nearestColor(c.R, c.G, c.B, p.Colors[:p.Degree]); got != k {
			t.Errorf("pixel (%d, %d) at root %d = %v is nearest to the color of root %d", x, y, k, c, got)
		}
		key := [3]uint8{c.R, c.G, c.B}
		if prev, ok := seen[key]; ok {
			t.Errorf("roots %d and %d have the same color %v", prev, k, c)
		}
		seen[key] = k
	}
}

// nearestColor returns the index of the color nearest to (r, g, b).
func nearestColor(r, g, b uint8, colors []config.Color) int {
	best, bestDist := -1, math.MaxInt
	for i, c := range colors {
		dr, dg, db := int(r)-int(c.R), int(g)-int(c.G), int(b)-int(c.B)
		if d := dr*dr + dg*dg + db*db; d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

func TestNewtonCopiesPreset(t *testing.T) {
	size := figure.Size{Width: 64, Height: 48}
	want, err := NewNewton(config.Default().Types.Newton, 1).Generate(t.Context(), size)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	p := config.Default().Types.Newton
	gen := NewNewton(p, 1)
	p.Colors[0] = config.Color{R: 0xff}
	p.Background = config.Color{G: 0xff}
	got, err := gen.Generate(t.Context(), size)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Error("changing the preset after NewNewton changed the image")
	}
}

func BenchmarkNewton(b *testing.B) {
	gen := NewNewton(config.Default().Types.Newton, 0)
	size := figure.Size{Width: 640, Height: 480}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := gen.Generate(b.Context(), size); err != nil {
			b.Fatal(err)
		}
	}
}
