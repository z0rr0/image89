package geometry

import (
	"context"
	"math"
)

const (
	// maxSagitta is the largest gap in pixels between a round join or cap and
	// the polygon that approximates it.
	maxSagitta = 0.25
	// maxChords bounds the chords of one join or cap, which matters only for
	// absurd widths: a half circle of radius 8192 px takes 202 chords.
	maxChords = 1024
	// strokeCheckEvery is the number of segments between two ctx checks.
	strokeCheckEvery = 1024
)

// segment is a segment of a stroked line: its unit direction and its ends
// shifted by the half width to the left (la, lb) and to the right (ra, rb).
// The shifted ends are computed once and shared by the rectangle of the
// segment, the joins and the caps.
type segment struct {
	dir            Point
	la, lb, ra, rb Point
}

// newSegment returns the segment from a to b, which must differ, for the half width r.
func newSegment(a, b Point, r float64) segment {
	dx, dy := b.X-a.X, b.Y-a.Y
	l := math.Hypot(dx, dy)
	ux, uy := dx/l, dy/l
	nx, ny := -uy*r, ux*r
	return segment{
		dir: Point{X: ux, Y: uy},
		la:  Point{X: a.X + nx, Y: a.Y + ny},
		lb:  Point{X: b.X + nx, Y: b.Y + ny},
		ra:  Point{X: a.X - nx, Y: a.Y - ny},
		rb:  Point{X: b.X - nx, Y: b.Y - ny},
	}
}

// Stroke returns polygons that cover the line pl drawn with the given width,
// in pixel coordinates. It approximates the union of the polygons that the
// rasterizer fills; their overlaps are covered once since the coverage is
// clamped.
//
// Every segment is a rectangle. At every vertex the outer side of the turn
// gets a bevel triangle, or a round sector instead when the bevel would be
// more than maxSagitta pixels from the arc; a turn by 180° gets a half disk.
// The ends of an open line get round caps. Joins and caps share the shifted
// ends of the segments as exact points, so there is no seam between them and
// the rectangles. The inner side of a turn is covered by the overlapping
// rectangles.
//
// Repeated points are dropped first (Dedupe); a line of fewer than two
// distinct points or a width that is not positive and finite gives no
// polygons. Stroke checks ctx every strokeCheckEvery segments.
func Stroke(ctx context.Context, pl Polyline, width float64) ([]Polygon, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pts := Dedupe(pl.Points, pl.Closed)
	r := width / 2
	if len(pts) < 2 || !(r > 0) || math.IsInf(r, 1) {
		return nil, nil
	}

	n := len(pts)
	segs := n - 1
	if pl.Closed {
		segs = n
	}
	// A rectangle has 6 points and a join usually 3.
	set := polygonSet{points: make([]Point, 0, 9*segs+16), ends: make([]int, 0, 2*segs+2)}

	first := newSegment(pts[0], pts[1], r)
	prev := first
	for i := range segs {
		if i%strokeCheckEvery == strokeCheckEvery-1 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		a, b := pts[i], pts[(i+1)%n]
		cur := first
		if i > 0 {
			cur = newSegment(a, b, r)
			set.join(a, prev, cur, r)
		}
		// The centers of the ends are vertices too, so every half of an end
		// edge matches an edge of a join or a cap point for point.
		set.add(cur.la, cur.lb, b, cur.rb, cur.ra, a)
		prev = cur
	}

	if pl.Closed {
		set.join(pts[0], prev, first, r)
	} else {
		set.arc(pts[0], first.la, first.ra, math.Pi, r)
		set.arc(pts[n-1], prev.rb, prev.lb, math.Pi, r)
	}
	return set.polygons(), nil
}

// join adds the join at the vertex v from the segment s1 to s2: a bevel or a
// round sector on the outer side of the turn.
func (s *polygonSet) join(v Point, s1, s2 segment, r float64) {
	cross := s1.dir.X*s2.dir.Y - s1.dir.Y*s2.dir.X
	dot := s1.dir.X*s2.dir.X + s1.dir.Y*s2.dir.Y
	theta := math.Atan2(cross, dot)
	if theta == 0 {
		return
	}
	// A turn towards the left side (theta > 0) has its outer side on the right.
	if theta > 0 {
		s.arc(v, s1.rb, s2.ra, theta, r)
	} else {
		s.arc(v, s1.lb, s2.la, theta, r)
	}
}

// arc adds the sector of the circle of radius r around c that starts at the
// point from and turns by theta to the point to. from and to lie on the circle
// and are used as given; the points between them are rotations of from. One
// chord gives a triangle, which is the bevel join.
func (s *polygonSet) arc(c, from, to Point, theta, r float64) {
	k := chords(r, math.Abs(theta))
	start := len(s.points)
	s.points = append(s.points, c, from)
	fx, fy := from.X-c.X, from.Y-c.Y
	for j := 1; j < k; j++ {
		sin, cos := math.Sincos(theta * float64(j) / float64(k))
		s.points = append(s.points, Point{X: c.X + fx*cos - fy*sin, Y: c.Y + fx*sin + fy*cos})
	}
	s.points = append(s.points, to)
	s.closePolygon(start)
}

// chords returns the number of equal chords that approximate an arc of radius
// r and angle theta with a gap of at most maxSagitta pixels, but not more than
// maxChords. A chord of angle φ is r·(1 − cos(φ/2)) = 2r·sin²(φ/4) away from the
// arc, so φ = 4·asin(√(maxSagitta / 2r)); this form keeps φ positive for a large r.
func chords(r, theta float64) int {
	q := maxSagitta / (2 * r)
	if q >= 1 {
		// Even a half circle is within maxSagitta of its diameter.
		return 1
	}
	k := math.Ceil(theta / (4 * math.Asin(math.Sqrt(q))))
	if !(k < maxChords) {
		return maxChords
	}
	return max(1, int(k))
}
