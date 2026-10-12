package geometry

import (
	"fmt"
	"math"
	"testing"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

func TestSierpinskiTriangles(t *testing.T) {
	t.Parallel()

	base := sqrt3 / 4 // the area of the triangle with the side 1
	for d := range 5 {
		tris := sierpinskiTriangles(d)
		if want := int(math.Pow(3, float64(d))); len(tris) != want {
			t.Errorf("depth %d: %d triangles, want %d", d, len(tris), want)
		}
		var area float64
		vertices := make([]Point, 0, 3*len(tris))
		for _, tri := range tris {
			area += area2(tri[:]) / 2
			vertices = append(vertices, tri[:]...)
		}
		if want := base * math.Pow(0.75, float64(d)); math.Abs(area-want) > 1e-12 {
			t.Errorf("depth %d: area %v, want %v", d, area, want)
		}
		if b, want := Bounds(vertices), (Rect{Max: Point{X: 1, Y: sqrt3 / 2}}); b != want {
			t.Errorf("depth %d: triangles span %v, want the base triangle %v", d, b, want)
		}
	}
}

func TestSierpinskiDepth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		preset int
		side   float64
		want   int
	}{
		{preset: 7, side: 3.9, want: 0},
		{preset: 7, side: 7.99, want: 0},
		{preset: 7, side: 8, want: 1},
		{preset: 7, side: 28.8, want: 2},
		{preset: 7, side: 512, want: 7},
		{preset: 7, side: 511, want: 6},
		{preset: 0, side: 1000, want: 0},
	}
	for _, tt := range tests {
		if got := sierpinskiDepth(tt.preset, tt.side); got != tt.want {
			t.Errorf("sierpinskiDepth(%d, %v) = %d, want %d", tt.preset, tt.side, got, tt.want)
		}
	}

	// The side of the fitted triangle limits the depth of the default preset.
	g := NewSierpinski(config.Default().Types.Sierpinski, 1)
	for size, want := range map[figure.Size]int{{Width: 32, Height: 32}: 2, {Width: 4096, Height: 4096}: 7} {
		side := g.fit(size).Scale
		if got := sierpinskiDepth(g.depth, side); got != want {
			t.Errorf("%v: side %.1f px gives depth %d, want %d", size, side, got, want)
		}
	}
	if side := g.fit(figure.Size{Width: 32, Height: 32}).Scale; math.Abs(side-28.8) > 1e-9 {
		t.Errorf("32x32: side %v px, want 28.8", side)
	}
}

// TestSierpinskiPixels checks pixels that lie well inside the triangles. The
// center of the image lies on the top side of the removed middle triangle, so
// the centroid of the big triangle is checked instead: it is the centroid of
// the removed triangle at every depth from 1 on.
func TestSierpinskiPixels(t *testing.T) {
	t.Parallel()

	for _, size := range []figure.Size{{Width: 128, Height: 128}, {Width: 160, Height: 120}, {Width: 120, Height: 160}} {
		for depth := 1; depth <= 3; depth++ {
			t.Run(fmt.Sprintf("%dx%d_depth%d", size.Width, size.Height, depth), func(t *testing.T) {
				t.Parallel()

				p := config.Default().Types.Sierpinski
				p.Depth = depth
				g := NewSierpinski(p, 1)
				img, err := g.Generate(t.Context(), size)
				if err != nil {
					t.Fatalf("Generate error: %v", err)
				}
				tr := g.fit(size)
				if c, want := at(img, tr.Apply(Point{X: 0.5, Y: sqrt3 / 6})), p.Background.RGBA(); c != want {
					t.Errorf("centroid pixel = %v, want the background %v", c, want)
				}
				// The centroid of the smallest triangle in the bottom left corner.
				s := math.Pow(0.5, float64(depth))
				if c, want := at(img, tr.Apply(Point{X: s / 2, Y: s * sqrt3 / 6})), p.Color.RGBA(); c != want {
					t.Errorf("corner triangle pixel = %v, want the color %v", c, want)
				}
			})
		}
	}
}
