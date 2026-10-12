package geometry

import (
	"context"
	"image"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

// kochMinSegment is the shortest segment of the Koch snowflake in pixels: a
// deeper level would draw details below it.
const kochMinSegment = 1

// Koch draws the outline of the Koch snowflake: every side of an equilateral
// triangle is replaced by four segments a third as long with a bump outside,
// again and again. The depth is the preset depth, reduced for small images so
// that the segments keep a length of at least kochMinSegment pixels.
type Koch struct {
	look  look
	depth int
	// bounds is the bounding box of the outline at every depth from 1 on: the
	// snowflake stays in the hexagon of the six points of depth 1.
	bounds Rect
}

// NewKoch returns a generator of the preset p, which must be valid, that uses
// workers goroutines per image (0 means GOMAXPROCS).
func NewKoch(p config.Koch, workers int) *Koch {
	return &Koch{
		look: look{
			color: p.Color.RGBA(), background: p.Background.RGBA(),
			padding: p.Padding, lineWidth: p.LineWidth, workers: workers,
		},
		depth:  p.Depth,
		bounds: Bounds(kochOutline(1).Points),
	}
}

// Generate draws an image of the given size. It returns a nil image and the
// error when ctx is done before the image is complete or the size is invalid.
func (g *Koch) Generate(ctx context.Context, size figure.Size) (*image.RGBA, error) {
	if err := start(ctx, size); err != nil {
		return nil, err
	}
	return g.look.stroke(ctx, size, kochOutline(g.depthFor(size)))
}

// depthFor returns the depth for an image of size. The side of the triangle
// in pixels is the scale of the fitted snowflake of depth 1, the same for
// every depth from 1 on.
func (g *Koch) depthFor(size figure.Size) int {
	return kochDepth(g.depth, g.look.strokeFit(g.bounds, size).Scale)
}

// kochDepth returns the largest depth up to preset at which the segments of a
// snowflake on a triangle with the side sidePx keep a length of at least
// kochMinSegment pixels, that is min(preset, floor(log3(sidePx))) and not
// below 0.
func kochDepth(preset int, sidePx float64) int {
	d := 0
	for s := sidePx / 3; d < preset && s >= kochMinSegment; s /= 3 {
		d++
	}
	return d
}

// kochOutline returns the closed outline of the Koch snowflake of the given
// depth, 3·4^depth points, built on the counterclockwise triangle (0, 0),
// (1, 0), (1/2, √3/2) in math coordinates.
func kochOutline(depth int) Polyline {
	const cos60, sin60 = 0.5, sqrt3 / 2
	pts := []Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0.5, Y: sqrt3 / 2}}
	for range depth {
		next := make([]Point, 0, 4*len(pts))
		for i, a := range pts {
			b := pts[(i+1)%len(pts)]
			dx, dy := (b.X-a.X)/3, (b.Y-a.Y)/3
			p1 := Point{X: a.X + dx, Y: a.Y + dy}
			// The bump is the third of the segment turned by −60°, to the
			// right, which is outside of a counterclockwise outline.
			peak := Point{X: p1.X + dx*cos60 + dy*sin60, Y: p1.Y - dx*sin60 + dy*cos60}
			p2 := Point{X: a.X + 2*dx, Y: a.Y + 2*dy}
			next = append(next, a, p1, peak, p2)
		}
		pts = next
	}
	return Polyline{Points: pts, Closed: true}
}
