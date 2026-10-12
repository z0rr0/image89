package geometry

import (
	"math"
	"testing"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

func TestKochOutline(t *testing.T) {
	t.Parallel()

	depth1 := Bounds(kochOutline(1).Points)
	for d := range 5 {
		line := kochOutline(d)
		if want := 3 * int(math.Pow(4, float64(d))); len(line.Points) != want {
			t.Errorf("depth %d: %d points, want %d", d, len(line.Points), want)
		}
		if !line.Closed {
			t.Errorf("depth %d: the outline is open", d)
		}
		// Every level adds 3·4^(d−1) bumps of area (1/9)^d of the triangle, so the
		// area is (8/5 − 3/5·(4/9)^d) of it and tends to 8/5. It is positive: the
		// outline is counterclockwise.
		if a, want := area2(line.Points)/2, sqrt3/4*(1.6-0.6*math.Pow(4.0/9, float64(d))); math.Abs(a-want) > 1e-12 {
			t.Errorf("depth %d: signed area %v, want %v", d, a, want)
		}
		if d == 0 {
			continue
		}
		// The bumps grow outside, but the bounds stay those of depth 1.
		b := Bounds(line.Points)
		if math.Abs(b.Min.X-depth1.Min.X) > 1e-12 || math.Abs(b.Min.Y-depth1.Min.Y) > 1e-12 ||
			math.Abs(b.Max.X-depth1.Max.X) > 1e-12 || math.Abs(b.Max.Y-depth1.Max.Y) > 1e-12 {
			t.Errorf("depth %d: bounds %v, want those of depth 1 %v", d, b, depth1)
		}
	}
}

func TestKochDepth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		preset int
		side   float64
		want   int
	}{
		{preset: 5, side: 0.5, want: 0},
		{preset: 5, side: 2.9, want: 0},
		{preset: 5, side: 3, want: 1},
		{preset: 5, side: 26.9, want: 2},
		{preset: 5, side: 27, want: 3},
		{preset: 5, side: 1e6, want: 5},
		{preset: 0, side: 1e6, want: 0},
	}
	for _, tt := range tests {
		if got := kochDepth(tt.preset, tt.side); got != tt.want {
			t.Errorf("kochDepth(%d, %v) = %d, want %d", tt.preset, tt.side, got, tt.want)
		}
	}

	g := NewKoch(config.Default().Types.Koch, 1)
	for size, want := range map[figure.Size]int{{Width: 32, Height: 32}: 2, {Width: 1024, Height: 1024}: 5} {
		if got := g.depthFor(size); got != want {
			t.Errorf("depthFor(%v) = %d, want %d", size, got, want)
		}
	}
}

func TestKochPixels(t *testing.T) {
	t.Parallel()

	p := config.Default().Types.Koch
	p.LineWidth = 0.02
	g := NewKoch(p, 1)
	for _, size := range []figure.Size{{Width: 512, Height: 512}, {Width: 640, Height: 480}} {
		img, err := g.Generate(t.Context(), size)
		if err != nil {
			t.Fatalf("Generate error: %v", err)
		}
		if c, want := img.RGBAAt(size.Width/2, size.Height/2), p.Background.RGBA(); c != want {
			t.Errorf("%v: center pixel = %v, want the background %v", size, c, want)
		}
		// The top vertex of the triangle stays the top of the snowflake; a
		// point w/4 below it lies inside the line.
		w := g.look.strokeWidth(size)
		tr := g.look.strokeFit(Bounds(kochOutline(g.depthFor(size)).Points), size)
		top := tr.Apply(Point{X: 0.5, Y: sqrt3 / 2})
		if c, want := at(img, Point{X: top.X, Y: top.Y + w/4}), p.Color.RGBA(); !near(c, want, 8) {
			t.Errorf("%v: pixel below the top vertex = %v, want about the color %v", size, c, want)
		}
	}
}
