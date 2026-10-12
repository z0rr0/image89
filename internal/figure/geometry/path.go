// Package geometry draws the contour figures: it builds their outlines in math
// coordinates, fits them into an image without distortion, turns lines into
// polygons of a given width and rasterizes the polygons in bands of rows.
package geometry

import (
	"math"
	"slices"

	"github.com/z0rr0/image89/internal/figure"
)

// Point is a point in math coordinates (Y grows up) or in pixel coordinates
// (Y grows down), depending on the stage.
type Point struct {
	X, Y float64
}

// Polyline is a line through Points; a closed one also joins the last point
// to the first.
type Polyline struct {
	Points []Point
	Closed bool
}

// Polygon is a closed contour to fill.
type Polygon []Point

// Rect is an axis-aligned bounding box.
type Rect struct {
	Min, Max Point
}

// Dx returns the width of r.
func (r Rect) Dx() float64 {
	return r.Max.X - r.Min.X
}

// Dy returns the height of r.
func (r Rect) Dy() float64 {
	return r.Max.Y - r.Min.Y
}

// Bounds returns the bounding box of points, the zero Rect for no points.
func Bounds(points []Point) Rect {
	if len(points) == 0 {
		return Rect{}
	}
	r := Rect{Min: points[0], Max: points[0]}
	for _, p := range points[1:] {
		r.Min.X, r.Min.Y = min(r.Min.X, p.X), min(r.Min.Y, p.Y)
		r.Max.X, r.Max.Y = max(r.Max.X, p.X), max(r.Max.Y, p.Y)
	}
	return r
}

// Dedupe returns a copy of points without the points equal to the previous
// kept one and, for a closed line, without the last points equal to the first
// one. Points are equal when their squared distance is zero, which also
// catches distances so small that the square underflows: a segment between
// such points has no direction.
func Dedupe(points []Point, closed bool) []Point {
	out := make([]Point, 0, len(points))
	for _, p := range points {
		if len(out) > 0 && dist2(out[len(out)-1], p) == 0 {
			continue
		}
		out = append(out, p)
	}
	for closed && len(out) > 1 && dist2(out[len(out)-1], out[0]) == 0 {
		out = out[:len(out)-1]
	}
	return out
}

// dist2 returns the squared distance between a and b.
func dist2(a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	return dx*dx + dy*dy
}

// Transform maps math coordinates to pixel coordinates with the same scale on
// both axes and a flipped Y axis: x' = X·Scale + DX, y' = DY − Y·Scale.
type Transform struct {
	Scale, DX, DY float64
}

// Apply returns p in pixel coordinates.
func (t Transform) Apply(p Point) Point {
	return Point{X: p.X*t.Scale + t.DX, Y: t.DY - p.Y*t.Scale}
}

// Fit returns the transform that scales bbox uniformly to the largest size
// that fits into an image of size and centers it. Every side keeps padding
// pixels free plus halfWidth for a stroke of width w = 2·halfWidth along the
// outline, so the scale is s = min((W − 2p − w)/Bw, (H − 2p − w)/Bh). An axis of
// bbox without extent does not limit the scale; a single point gets the scale 1.
// The caller keeps 2p + w below both sides, so the scale is positive.
func Fit(bbox Rect, size figure.Size, padding, halfWidth float64) Transform {
	width, height := float64(size.Width), float64(size.Height)
	margin := 2*padding + 2*halfWidth
	scale := math.Inf(1)
	if dx := bbox.Dx(); dx > 0 {
		scale = (width - margin) / dx
	}
	if dy := bbox.Dy(); dy > 0 {
		scale = min(scale, (height-margin)/dy)
	}
	if math.IsInf(scale, 1) {
		scale = 1
	}
	cx, cy := (bbox.Min.X+bbox.Max.X)/2, (bbox.Min.Y+bbox.Max.Y)/2
	return Transform{Scale: scale, DX: width/2 - cx*scale, DY: height/2 + cy*scale}
}

// area2 returns twice the signed area of the polygon (the shoelace formula).
func area2(poly []Point) float64 {
	var s float64
	prev := poly[len(poly)-1]
	for _, p := range poly {
		s += prev.X*p.Y - p.X*prev.Y
		prev = p
	}
	return s
}

// polygonSet collects polygons in one backing array of points, so building
// many small polygons does not allocate every one of them.
//
// Every polygon gets a positive signed area. The rasterizer sums signed
// coverage, so polygons of opposite orientations would cancel each other where
// they overlap. With one orientation two polygons that share an edge traverse
// it in opposite directions, and the contributions of that edge cancel
// exactly if both polygons take its ends from the same Point values.
type polygonSet struct {
	points []Point
	ends   []int
}

// add appends the polygon pts.
func (s *polygonSet) add(pts ...Point) {
	start := len(s.points)
	s.points = append(s.points, pts...)
	s.closePolygon(start)
}

// closePolygon turns the points from start to the end into a polygon: it
// reverses them if their signed area is negative and drops them if the area is
// zero or not a number.
func (s *polygonSet) closePolygon(start int) {
	poly := s.points[start:]
	if len(poly) < 3 {
		s.points = s.points[:start]
		return
	}
	a := area2(poly)
	if a == 0 || math.IsNaN(a) || math.IsInf(a, 0) {
		s.points = s.points[:start]
		return
	}
	if a < 0 {
		slices.Reverse(poly)
	}
	s.ends = append(s.ends, len(s.points))
}

// polygons returns the collected polygons; they share the backing array.
func (s *polygonSet) polygons() []Polygon {
	if len(s.ends) == 0 {
		return nil
	}
	polys := make([]Polygon, len(s.ends))
	start := 0
	for i, end := range s.ends {
		polys[i] = Polygon(s.points[start:end:end])
		start = end
	}
	return polys
}
