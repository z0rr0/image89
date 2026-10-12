package geometry

import (
	"math"
	"testing"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

func TestLissajousCircle(t *testing.T) {
	t.Parallel()

	// a = b = 1 and δ = π/2 give x = cos t, y = sin t.
	pts := lissajousPoints(1, 1, math.Pi/2, 100)
	for i, p := range pts {
		if r := math.Hypot(p.X, p.Y); math.Abs(r-1) > 1e-9 {
			t.Errorf("point %d %v is %v from the origin, want 1", i, p, r)
		}
	}
}

// TestLissajousLargePhase checks a phase that the config accepts but that
// would absorb a·t in the sum a·t + δ and flatten the curve into a line.
func TestLissajousLargePhase(t *testing.T) {
	t.Parallel()

	const n = 1024
	for _, delta := range []float64{1e20, -1e300} {
		pts := lissajousPoints(3, 2, delta, n)
		if dx := Bounds(pts).Dx(); dx < 1.99 {
			t.Errorf("delta %g: the curve spans %v along X, want about 2", delta, dx)
		}
		sinDelta, cosDelta := math.Sincos(delta)
		for i, p := range pts {
			at := 3 * 2 * math.Pi * float64(i) / n
			if want := math.Sin(at)*cosDelta + math.Cos(at)*sinDelta; math.Abs(p.X-want) > 1e-12 {
				t.Fatalf("delta %g: point %d has x %v, want %v", delta, i, p.X, want)
			}
		}
	}
}

func TestLissajousPeriod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b   int
		period float64
	}{
		{a: 2, b: 4, period: math.Pi},
		{a: 3, b: 2, period: 2 * math.Pi},
		{a: 6, b: 9, period: 2 * math.Pi / 3},
	}
	const n, delta = 64, 0.3
	for _, tt := range tests {
		pts := lissajousPoints(tt.a, tt.b, delta, n)
		if len(pts) != n {
			t.Fatalf("a=%d b=%d: %d points, want %d", tt.a, tt.b, len(pts), n)
		}
		for i, p := range pts {
			tt0 := tt.period * float64(i) / n
			want := Point{X: math.Sin(float64(tt.a)*tt0 + delta), Y: math.Sin(float64(tt.b) * tt0)}
			if math.Abs(p.X-want.X) > 1e-12 || math.Abs(p.Y-want.Y) > 1e-12 {
				t.Errorf("a=%d b=%d: point %d = %v, want %v at t = %v", tt.a, tt.b, i, p, want, tt0)
			}
		}
		// The point after the last one closes the curve and is not repeated.
		if got := Dedupe(pts, true); len(got) != n {
			t.Errorf("a=%d b=%d: %d distinct points of %d", tt.a, tt.b, len(got), n)
		}
	}
}

func TestLissajousCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		points, a, b int
		size         figure.Size
		want         int
	}{
		{name: "small", a: 3, b: 2, size: figure.Size{Width: 32, Height: 32}, want: minLissajousPoints},
		{name: "1024", a: 3, b: 2, size: figure.Size{Width: 1024, Height: 1024}, want: 4826},
		{name: "min side", a: 1, b: 32, size: figure.Size{Width: 65536, Height: 32}, want: 1609},
		{name: "large", a: 32, b: 1, size: figure.Size{Width: 65536, Height: 65536}, want: maxLissajousPoints},
		{name: "preset", points: 500, a: 3, b: 2, size: figure.Size{Width: 1024, Height: 1024}, want: 500},
	}
	for _, tt := range tests {
		if got := lissajousCount(tt.points, tt.a, tt.b, tt.size); got != tt.want {
			t.Errorf("%s: lissajousCount = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestLissajousPixels(t *testing.T) {
	t.Parallel()

	p := config.Default().Types.Lissajous
	p.LineWidth = 0.02
	g := NewLissajous(p, 1)
	for _, size := range []figure.Size{{Width: 512, Height: 512}, {Width: 640, Height: 480}} {
		img, err := g.Generate(t.Context(), size)
		if err != nil {
			t.Fatalf("Generate error: %v", err)
		}
		pts := lissajousPoints(p.A, p.B, p.Delta, lissajousCount(p.Points, p.A, p.B, size))
		tr := g.look.strokeFit(Bounds(pts), size)
		// The tangent at t = 0 is (a·cos δ, b), with the Y axis flipped in pixels.
		tx, ty := float64(p.A)*math.Cos(p.Delta), -float64(p.B)
		l := math.Hypot(tx, ty)
		shift := g.look.strokeWidth(size) / 4
		start := tr.Apply(pts[0])
		inner := Point{X: start.X - ty/l*shift, Y: start.Y + tx/l*shift}
		if c, want := at(img, inner), p.Color.RGBA(); !near(c, want, 8) {
			t.Errorf("%v: pixel w/4 off the point of t = 0 = %v, want about the color %v", size, c, want)
		}
	}
}
