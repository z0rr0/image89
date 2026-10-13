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
	// minTurn is the smallest turn in radians that gets a join. turn of two
	// unit vectors carries rounding noise of about 1e-15, so a straight vertex
	// (an inflection point of a curve) may get a turn of that size and a join
	// of zero area, which closePolygon drops or keeps depending on FMA. The
	// bevel of a real turn below minTurn is less than r·1e-12 px wide.
	minTurn = 1e-12
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
	dir := direction(a, b)
	ux, uy := dir.X, dir.Y
	nx, ny := -uy*r, ux*r
	return segment{
		dir: dir,
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
	phi := chordAngle(r)
	size, err := strokePoints(ctx, pts, segs, pl.Closed, phi)
	if err != nil {
		return nil, err
	}
	set := polygonSet{points: make([]Point, 0, size), ends: make([]int, 0, 2*segs+2)}

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
			set.join(a, prev, cur, phi)
		}
		// The centers of the ends are vertices too, so every half of an end
		// edge matches an edge of a join or a cap point for point.
		set.add(cur.la, cur.lb, b, cur.rb, cur.ra, a)
		prev = cur
	}

	if pl.Closed {
		set.join(pts[0], prev, first, phi)
	} else {
		set.arc(pts[0], first.la, first.ra, math.Pi, phi)
		set.arc(pts[n-1], prev.rb, prev.lb, math.Pi, phi)
	}
	return set.polygons(), nil
}

// direction returns the unit vector from a to b, which must differ.
func direction(a, b Point) Point {
	dx, dy := b.X-a.X, b.Y-a.Y
	l := math.Hypot(dx, dy)
	return Point{X: dx / l, Y: dy / l}
}

// turn returns the signed angle from the direction d1 to d2.
func turn(d1, d2 Point) float64 {
	return math.Atan2(d1.X*d2.Y-d1.Y*d2.X, d1.X*d2.X+d1.Y*d2.Y)
}

// strokePoints returns how many points Stroke adds at most for the segs
// segments of pts with chords of at most the angle phi: 6 for every rectangle
// and k + 2 for every join or cap of k chords. The polygons that closePolygon drops make
// the actual number smaller. One pass over the turns costs less than growing
// the points by append, which copies them and keeps the old arrays until
// the next GC. It checks ctx every strokeCheckEvery segments, so a canceled
// line allocates no points.
func strokePoints(ctx context.Context, pts []Point, segs int, closed bool, phi float64) (int, error) {
	arc := func(theta float64) int {
		if math.Abs(theta) < minTurn {
			return 0
		}
		return chords(phi, math.Abs(theta)) + 2
	}
	n := len(pts)
	total := 6 * segs
	prev := direction(pts[0], pts[1])
	first := prev
	for i := 1; i < segs; i++ {
		if i%strokeCheckEvery == strokeCheckEvery-1 {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
		}
		cur := direction(pts[i], pts[(i+1)%n])
		total += arc(turn(prev, cur))
		prev = cur
	}
	if closed {
		return total + arc(turn(prev, first)), nil
	}
	return total + 2*arc(math.Pi), nil
}

// join adds the join at the vertex v from the segment s1 to s2: a bevel or a
// round sector of chords of at most the angle phi on the outer side of the turn.
func (s *polygonSet) join(v Point, s1, s2 segment, phi float64) {
	theta := turn(s1.dir, s2.dir)
	if math.Abs(theta) < minTurn {
		return
	}
	// A turn towards the left side (theta > 0) has its outer side on the right.
	if theta > 0 {
		s.arc(v, s1.rb, s2.ra, theta, phi)
	} else {
		s.arc(v, s1.lb, s2.la, theta, phi)
	}
}

// arc adds the sector of a circle around c that starts at the point from and
// turns by theta to the point to, in chords of at most the angle phi. from
// and to lie on the circle and are used as given; the points between them are
// rotations of from. One chord gives a triangle, which is the bevel join.
func (s *polygonSet) arc(c, from, to Point, theta, phi float64) {
	k := chords(phi, math.Abs(theta))
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

// chordAngle returns the largest angle of a chord of a circle of radius r
// that leaves a gap of at most maxSagitta pixels to the arc. A chord of angle
// φ is r·(1 − cos(φ/2)) = 2r·sin²(φ/4) away from the arc, so
// φ = 4·asin(√(maxSagitta / 2r)); this form keeps φ positive for a large r.
// A small circle gives +Inf: even its half is within maxSagitta of the diameter.
func chordAngle(r float64) float64 {
	q := maxSagitta / (2 * r)
	if q >= 1 {
		return math.Inf(1)
	}
	return 4 * math.Asin(math.Sqrt(q))
}

// chords returns the number of equal chords of at most the angle phi (see
// chordAngle) that approximate an arc of the angle theta: at least 1 and at
// most maxChords.
func chords(phi, theta float64) int {
	k := math.Ceil(theta / phi)
	if !(k < maxChords) {
		return maxChords
	}
	return max(1, int(k))
}
