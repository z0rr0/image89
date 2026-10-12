package geometry

import (
	"context"
	"image"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

const (
	// sqrt3 is √3; the height of an equilateral triangle with the side 1 is sqrt3/2.
	sqrt3 = 1.7320508075688772
	// sierpinskiMinSide is the smallest side in pixels of a triangle of the
	// Sierpinski figure: a deeper level would draw details below it.
	sierpinskiMinSide = 4
)

// Sierpinski draws the Sierpinski triangle: an equilateral triangle split
// again and again into three corner triangles with the middle one removed.
// The depth is the preset depth, reduced for small images so that the
// smallest triangles keep sides of at least sierpinskiMinSide pixels.
type Sierpinski struct {
	look  look
	depth int
}

// NewSierpinski returns a generator of the preset p, which must be valid,
// that uses workers goroutines per image (0 means GOMAXPROCS).
func NewSierpinski(p config.Sierpinski, workers int) *Sierpinski {
	return &Sierpinski{
		look:  look{color: p.Color.RGBA(), background: p.Background.RGBA(), padding: p.Padding, workers: workers},
		depth: p.Depth,
	}
}

// Generate draws an image of the given size. It returns a nil image and the
// error when ctx is done before the image is complete or the size is invalid.
func (g *Sierpinski) Generate(ctx context.Context, size figure.Size) (*image.RGBA, error) {
	if err := start(ctx, size); err != nil {
		return nil, err
	}
	t := g.fit(size)
	tris := sierpinskiTriangles(sierpinskiDepth(g.depth, t.Scale))
	set := polygonSet{points: make([]Point, 0, 3*len(tris)), ends: make([]int, 0, len(tris))}
	for _, tri := range tris {
		set.add(t.Apply(tri[0]), t.Apply(tri[1]), t.Apply(tri[2]))
	}
	return g.look.paint(ctx, size, set.polygons())
}

// fit returns the transform of the triangle with the side 1 into size; its
// scale is the side of the triangle in pixels. The figure has no line, so
// only the padding is kept free.
func (g *Sierpinski) fit(size figure.Size) Transform {
	return Fit(Rect{Max: Point{X: 1, Y: sqrt3 / 2}}, size, g.look.paddingPx(size), 0)
}

// sierpinskiDepth returns the largest depth up to preset at which the
// triangles of a figure with the side sidePx keep sides of at least
// sierpinskiMinSide pixels, that is min(preset, floor(log2(sidePx / 4))) and
// not below 0.
func sierpinskiDepth(preset int, sidePx float64) int {
	d := 0
	for s := sidePx / 2; d < preset && s >= sierpinskiMinSide; s /= 2 {
		d++
	}
	return d
}

// sierpinskiTriangles returns the 3^depth triangles of the Sierpinski figure
// built on the triangle (0, 0), (1, 0), (1/2, √3/2) in math coordinates.
func sierpinskiTriangles(depth int) [][3]Point {
	count := 1
	for range depth {
		count *= 3
	}
	tris := make([][3]Point, 0, count)
	var split func(a, b, c Point, d int)
	split = func(a, b, c Point, d int) {
		if d == 0 {
			tris = append(tris, [3]Point{a, b, c})
			return
		}
		ab, bc, ca := midpoint(a, b), midpoint(b, c), midpoint(c, a)
		split(a, ab, ca, d-1)
		split(ab, b, bc, d-1)
		split(ca, bc, c, d-1)
	}
	split(Point{X: 0, Y: 0}, Point{X: 1, Y: 0}, Point{X: 0.5, Y: sqrt3 / 2}, depth)
	return tris
}

// midpoint returns the middle of the segment ab.
func midpoint(a, b Point) Point {
	return Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
}
