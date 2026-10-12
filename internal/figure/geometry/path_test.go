package geometry

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/z0rr0/image89/internal/figure"
)

func TestBounds(t *testing.T) {
	t.Parallel()

	if got := Bounds(nil); got != (Rect{}) {
		t.Errorf("Bounds(nil) = %v, want the zero Rect", got)
	}
	got := Bounds([]Point{{X: 1, Y: 2}, {X: -3, Y: 5}, {X: 4, Y: -1}})
	want := Rect{Min: Point{X: -3, Y: -1}, Max: Point{X: 4, Y: 5}}
	if got != want {
		t.Errorf("Bounds = %v, want %v", got, want)
	}
	if got.Dx() != 7 || got.Dy() != 6 {
		t.Errorf("Dx, Dy = %v, %v; want 7, 6", got.Dx(), got.Dy())
	}
}

func TestDedupe(t *testing.T) {
	t.Parallel()

	a, b, c := Point{X: 0, Y: 0}, Point{X: 1, Y: 0}, Point{X: 1, Y: 1}
	tiny := Point{X: 1e-200, Y: 0} // its squared distance from a underflows to zero
	tests := []struct {
		name   string
		points []Point
		closed bool
		want   []Point
	}{
		{name: "empty", points: nil, want: []Point{}},
		{name: "distinct", points: []Point{a, b, c}, want: []Point{a, b, c}},
		{name: "repeated", points: []Point{a, a, b, b, b, c, c}, want: []Point{a, b, c}},
		{name: "underflow", points: []Point{a, tiny, b}, want: []Point{a, b}},
		{name: "open keeps the closing point", points: []Point{a, b, c, a}, want: []Point{a, b, c, a}},
		{name: "closed drops the closing point", points: []Point{a, b, c, a, a}, closed: true, want: []Point{a, b, c}},
		{name: "all equal", points: []Point{a, a, a}, closed: true, want: []Point{a}},
		{name: "not consecutive", points: []Point{a, b, a, b}, want: []Point{a, b, a, b}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := slices.Clone(tt.points)
			got := Dedupe(in, tt.closed)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Dedupe = %v, want %v", got, tt.want)
			}
			if !slices.Equal(in, tt.points) {
				t.Error("Dedupe changed its argument")
			}
		})
	}
}

func TestFit(t *testing.T) {
	t.Parallel()

	// The bounds of the Koch snowflake: wider than high in no image.
	bbox := Rect{Min: Point{X: 0, Y: -0.2887}, Max: Point{X: 1, Y: 0.866}}
	const padding, halfWidth = 10.0, 3.0
	for _, size := range []figure.Size{{Width: 640, Height: 480}, {Width: 480, Height: 640}, {Width: 640, Height: 640}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			t.Parallel()

			tr := Fit(bbox, size, padding, halfWidth)
			lo, hi := tr.Apply(bbox.Min), tr.Apply(bbox.Max)
			// One scale on both axes and the Y axis flipped.
			if dx, dy := hi.X-lo.X, lo.Y-hi.Y; math.Abs(dx-bbox.Dx()*tr.Scale) > 1e-9 || math.Abs(dy-bbox.Dy()*tr.Scale) > 1e-9 {
				t.Errorf("image of bbox is %v × %v, want both at scale %v", dx, dy, tr.Scale)
			}
			// The bbox and the half width stay within the padding.
			w, h := float64(size.Width), float64(size.Height)
			limit := padding + halfWidth - 1e-9
			if lo.X < limit || hi.Y < limit || w-hi.X < limit || h-lo.Y < limit {
				t.Errorf("bbox at x [%v, %v], y [%v, %v] leaves less than %v px to the borders of %v", lo.X, hi.X, hi.Y, lo.Y, limit, size)
			}
			// The scale is the largest one: the bbox touches one pair of borders.
			if math.Abs(min(lo.X, h-lo.Y)-padding-halfWidth) > 1e-9 {
				t.Errorf("bbox does not touch the borders: x [%v, %v], y [%v, %v]", lo.X, hi.X, hi.Y, lo.Y)
			}
			center := tr.Apply(Point{X: (bbox.Min.X + bbox.Max.X) / 2, Y: (bbox.Min.Y + bbox.Max.Y) / 2})
			if math.Abs(center.X-w/2) > 1e-9 || math.Abs(center.Y-h/2) > 1e-9 {
				t.Errorf("center of bbox maps to %v, want (%v, %v)", center, w/2, h/2)
			}
		})
	}
}

func TestFitDegenerate(t *testing.T) {
	t.Parallel()

	size := figure.Size{Width: 200, Height: 100}
	tests := []struct {
		name  string
		bbox  Rect
		scale float64
	}{
		{name: "horizontal segment", bbox: Rect{Max: Point{X: 2}}, scale: 95},
		{name: "vertical segment", bbox: Rect{Max: Point{Y: 3}}, scale: 30},
		{name: "point", bbox: Rect{Min: Point{X: 5, Y: 5}, Max: Point{X: 5, Y: 5}}, scale: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := Fit(tt.bbox, size, 5, 0)
			if tr.Scale != tt.scale {
				t.Errorf("scale = %v, want %v", tr.Scale, tt.scale)
			}
			if c := tr.Apply(tt.bbox.Min); math.IsNaN(c.X) || math.IsNaN(c.Y) || math.IsInf(c.X, 0) || math.IsInf(c.Y, 0) {
				t.Errorf("Apply(%v) = %v, want finite", tt.bbox.Min, c)
			}
		})
	}
}

func TestPolygonSet(t *testing.T) {
	t.Parallel()

	a, b, c := Point{X: 0, Y: 0}, Point{X: 4, Y: 0}, Point{X: 0, Y: 3}
	nan := Point{X: math.NaN(), Y: 1}
	var set polygonSet
	set.add(a, b, c)                 // positive
	set.add(a, c, b)                 // negative, reversed
	set.add(a, b, Point{X: 8, Y: 0}) // collinear, dropped
	set.add(a, b)                    // too short, dropped
	set.add(a, b, nan)               // not a number, dropped
	set.add(c, Point{X: 1, Y: 1}, b) // positive
	set.add(c, Point{X: 5, Y: 5}, b) // negative, reversed

	polys := set.polygons()
	if len(polys) != 4 {
		t.Fatalf("got %d polygons, want 4: %v", len(polys), polys)
	}
	for i, poly := range polys {
		if a := area2(poly); a <= 0 {
			t.Errorf("polygon %d %v has signed area %v, want positive", i, poly, a/2)
		}
		if cap(poly) != len(poly) {
			t.Errorf("polygon %d has capacity %d, want its length %d", i, cap(poly), len(poly))
		}
	}
	if want := (Polygon{b, c, a}); !slices.Equal(polys[1], want) {
		t.Errorf("reversed polygon = %v, want %v", polys[1], want)
	}
	if got := (&polygonSet{}).polygons(); got != nil {
		t.Errorf("empty set gives %v, want nil", got)
	}
}
