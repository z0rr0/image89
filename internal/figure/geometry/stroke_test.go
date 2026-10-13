package geometry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
)

func TestStrokeSegmentArea(t *testing.T) {
	t.Parallel()

	tests := []struct{ length, width float64 }{{100, 10}, {100, 4}, {60, 20}, {100, 1}}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("L%v_w%v", tt.length, tt.width), func(t *testing.T) {
			t.Parallel()

			line := Polyline{Points: []Point{{X: 50, Y: 50}, {X: 50 + tt.length, Y: 50}}}
			polys, err := Stroke(t.Context(), line, tt.width)
			if err != nil {
				t.Fatalf("Stroke error: %v", err)
			}
			r := tt.width / 2
			want := tt.length*tt.width + math.Pi*r*r
			if got := coverage(rasterize(t, polys, 200, 100)); math.Abs(got-want) > 0.05*want {
				t.Errorf("painted area = %.1f, want %.1f ± 5%%", got, want)
			}
		})
	}
}

// strokeCases are lines that cover the hard cases of joins: sharp and
// reversed turns, self-intersections and segments shorter than the width.
func strokeCases() map[string]Polyline {
	zigzag := make([]Point, 0, 40)
	for i := range 40 {
		zigzag = append(zigzag, Point{X: 40 + 3*float64(i), Y: 100 + 4*float64(i%2)})
	}
	return map[string]Polyline{
		"square":   {Points: []Point{{X: 50, Y: 50}, {X: 150, Y: 50}, {X: 150, Y: 150}, {X: 50, Y: 150}}, Closed: true},
		"hairpin":  {Points: []Point{{X: 40, Y: 100}, {X: 160, Y: 100}, {X: 40, Y: 101}}},
		"reversed": {Points: []Point{{X: 40, Y: 100}, {X: 160, Y: 100}, {X: 40, Y: 100}}},
		"bowtie":   {Points: []Point{{X: 40, Y: 40}, {X: 160, Y: 160}, {X: 160, Y: 40}, {X: 40, Y: 160}}, Closed: true},
		"zigzag":   {Points: zigzag},
		"spiral": {Points: func() []Point {
			pts := make([]Point, 0, 200)
			for i := range 200 {
				a, r := float64(i)*0.3, 10+float64(i)*0.4
				pts = append(pts, Point{X: 100 + r*math.Cos(a), Y: 100 + r*math.Sin(a)})
			}
			return pts
		}()},
	}
}

func TestStrokeOrientation(t *testing.T) {
	t.Parallel()

	for name, line := range strokeCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			polys, err := Stroke(t.Context(), line, 10)
			if err != nil {
				t.Fatalf("Stroke error: %v", err)
			}
			if len(polys) == 0 {
				t.Fatal("no polygons")
			}
			for i, poly := range polys {
				a := area2(poly)
				if len(poly) < 3 || !(a > 0) || math.IsInf(a, 0) {
					t.Errorf("polygon %d of %d points has signed area %v, want positive", i, len(poly), a/2)
				}
			}
		})
	}
}

// TestStrokeCoverage checks the union approximation on the image: every pixel
// whose center is closer than w/2 − 1 to the line is painted fully, so there
// are no holes and no canceled overlaps, and every pixel farther than w/2 + 1
// is untouched.
func TestStrokeCoverage(t *testing.T) {
	t.Parallel()

	const width = 10.0
	for name, line := range strokeCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			polys, err := Stroke(t.Context(), line, width)
			if err != nil {
				t.Fatalf("Stroke error: %v", err)
			}
			img := rasterize(t, polys, 200, 200)
			for y := range 200 {
				for x := range 200 {
					d := distToLine(Point{X: float64(x) + 0.5, Y: float64(y) + 0.5}, line)
					c := img.RGBAAt(x, y)
					if d < width/2-1 && c.R > 1 {
						t.Fatalf("pixel (%d, %d) at %.2f from the line has %v, want painted", x, y, d, c)
					}
					if d > width/2+1 && c.R != 0xff {
						t.Fatalf("pixel (%d, %d) at %.2f from the line has %v, want the background", x, y, d, c)
					}
				}
			}
		})
	}
}

// TestStrokePoints checks that the capacity Stroke allocates holds all its
// points, so the points never grow by append: the count is exact when no
// polygon is dropped and an upper bound otherwise. A straight vertex, such as
// the inflection points of the Lissajous curve at t = π/2 and 3π/2, has a turn
// of rounding noise below minTurn and gets no join in both, so koch and
// lissajous are exact on every platform.
func TestStrokePoints(t *testing.T) {
	t.Parallel()

	cases := strokeCases()
	cases["koch"] = Polyline{Points: scale(kochOutline(4).Points, 180, 10), Closed: true}
	cases["lissajous"] = Polyline{Points: scale(lissajousPoints(3, 2, math.Pi/2, 500), 90, 100), Closed: true}
	for name, line := range cases {
		for _, width := range []float64{1, 10, 60} {
			polys, err := Stroke(t.Context(), line, width)
			if err != nil {
				t.Fatalf("%s: Stroke error: %v", name, err)
			}
			got := 0
			for _, poly := range polys {
				got += len(poly)
			}
			pts := Dedupe(line.Points, line.Closed)
			segs := len(pts) - 1
			if line.Closed {
				segs = len(pts)
			}
			want, err := strokePoints(t.Context(), pts, segs, line.Closed, chordAngle(width/2))
			if err != nil {
				t.Fatalf("%s: strokePoints error: %v", name, err)
			}
			if got > want || (got != want && (name == "koch" || name == "lissajous")) {
				t.Errorf("%s width %v: Stroke has %d points, strokePoints = %d", name, width, got, want)
			}
		}
	}
}

// scale returns pts scaled by k and shifted by d along both axes.
func scale(pts []Point, k, d float64) []Point {
	out := make([]Point, len(pts))
	for i, p := range pts {
		out[i] = Point{X: d + k*p.X, Y: d + k*p.Y}
	}
	return out
}

func TestStrokeJoins(t *testing.T) {
	t.Parallel()

	const width = 20.0
	r := width / 2
	v := Point{X: 100, Y: 100}
	tests := []struct {
		name   string
		degree float64
		round  bool
	}{
		{name: "10°", degree: 10},
		{name: "90°", degree: 90, round: true},
		{name: "-90°", degree: -90, round: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			turn := tt.degree * math.Pi / 180
			end := Point{X: v.X + 50*math.Cos(turn), Y: v.Y + 50*math.Sin(turn)}
			polys, err := Stroke(t.Context(), Polyline{Points: []Point{{X: 50, Y: 100}, v, end}}, width)
			if err != nil {
				t.Fatalf("Stroke error: %v", err)
			}
			bevels := 0
			for _, poly := range polys {
				if len(poly) == 3 {
					bevels++
				}
			}
			// The outer bisector points away from both segments.
			bisector := turn/2 - math.Copysign(math.Pi/2, turn)
			far := Point{X: v.X + 0.95*r*math.Cos(bisector), Y: v.Y + 0.95*r*math.Sin(bisector)}
			if tt.round {
				if bevels != 0 || !inside(polys, far) {
					t.Errorf("got %d bevels, point %v at 0.95·r covered: %v; want a round join", bevels, far, inside(polys, far))
				}
				return
			}
			if bevels != 1 {
				t.Errorf("got %d bevel triangles, want 1", bevels)
			}
		})
	}
}

func TestChords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		r, theta float64
		want     int
	}{
		{r: 0.1, theta: math.Pi, want: 1},
		{r: 0.125, theta: math.Pi, want: 1},
		{r: 10, theta: 10 * math.Pi / 180, want: 1},
		{r: 10, theta: math.Pi / 2, want: 4},
		{r: 8192, theta: math.Pi, want: 202},
		{r: 1e300, theta: math.Pi, want: maxChords},
	}

	for _, tt := range tests {
		if got := chords(chordAngle(tt.r), tt.theta); got != tt.want {
			t.Errorf("chords(%v, %v) = %d, want %d", tt.r, tt.theta, got, tt.want)
		}
		// Every chord of the result is within maxSagitta of the arc.
		if got := chords(chordAngle(tt.r), tt.theta); got < maxChords {
			if gap := tt.r * (1 - math.Cos(tt.theta/float64(got)/2)); gap > maxSagitta+1e-12 && tt.r > maxSagitta/2 {
				t.Errorf("chords(%v, %v) = %d leaves a gap of %v px", tt.r, tt.theta, got, gap)
			}
		}
	}
}

func TestStrokeDegenerate(t *testing.T) {
	t.Parallel()

	a, b, c := Point{X: 10, Y: 10}, Point{X: 50, Y: 10}, Point{X: 50, Y: 50}
	polys, err := Stroke(t.Context(), Polyline{Points: []Point{a, a, b, b, b, c, c}}, 6)
	if err != nil {
		t.Fatalf("Stroke error: %v", err)
	}
	// Two rectangles, one join and two caps.
	if len(polys) != 5 {
		t.Errorf("got %d polygons, want 5", len(polys))
	}
	for i, poly := range polys {
		if a := area2(poly); !(a > 0) || math.IsInf(a, 0) {
			t.Errorf("polygon %d has signed area %v, want positive", i, a/2)
		}
	}

	// A point on a straight line needs no join: two rectangles and two caps.
	straight, err := Stroke(t.Context(), Polyline{Points: []Point{a, b, {X: 90, Y: 10}}}, 6)
	if err != nil || len(straight) != 4 {
		t.Errorf("straight line: %d polygons, %v; want 4", len(straight), err)
	}

	tests := []struct {
		name  string
		line  Polyline
		width float64
	}{
		{name: "empty", width: 4},
		{name: "one point", line: Polyline{Points: []Point{a}}, width: 4},
		{name: "equal points", line: Polyline{Points: []Point{a, a, a}, Closed: true}, width: 4},
		{name: "zero width", line: Polyline{Points: []Point{a, b}}, width: 0},
		{name: "negative width", line: Polyline{Points: []Point{a, b}}, width: -1},
		{name: "NaN width", line: Polyline{Points: []Point{a, b}}, width: math.NaN()},
		{name: "infinite width", line: Polyline{Points: []Point{a, b}}, width: math.Inf(1)},
	}
	for _, tt := range tests {
		polys, err := Stroke(t.Context(), tt.line, tt.width)
		if err != nil || polys != nil {
			t.Errorf("%s: Stroke = %d polygons, %v; want none", tt.name, len(polys), err)
		}
	}
}

func TestStrokeCanceled(t *testing.T) {
	t.Parallel()

	line := Polyline{Points: make([]Point, 3*strokeCheckEvery)}
	for i := range line.Points {
		line.Points[i] = Point{X: float64(i), Y: float64(i % 2)}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if polys, err := Stroke(ctx, line, 4); !errors.Is(err, context.Canceled) || polys != nil {
		t.Errorf("Stroke with a canceled ctx = %d polygons, %v; want %v", len(polys), err, context.Canceled)
	}
	// The first check passes, the check of strokePoints stops it.
	if polys, err := Stroke(newCountdown(1), line, 4); !errors.Is(err, context.Canceled) || polys != nil {
		t.Errorf("Stroke canceled while counting = %d polygons, %v; want %v", len(polys), err, context.Canceled)
	}
	// The first check and both checks of strokePoints (3071 segments) pass, the
	// check inside the loop stops it.
	if polys, err := Stroke(newCountdown(3), line, 4); !errors.Is(err, context.Canceled) || polys != nil {
		t.Errorf("Stroke canceled in the loop = %d polygons, %v; want %v", len(polys), err, context.Canceled)
	}
}

// TestStrokeCanceledAllocs checks that a line canceled while its points are
// counted allocates no points: only the context and the copy of Dedupe. It is
// sequential, because AllocsPerRun is.
func TestStrokeCanceledAllocs(t *testing.T) {
	line := Polyline{Points: make([]Point, 3*strokeCheckEvery)}
	for i := range line.Points {
		line.Points[i] = Point{X: float64(i), Y: float64(i % 2)}
	}
	var err error
	allocs := testing.AllocsPerRun(1, func() {
		_, err = Stroke(newCountdown(1), line, 4)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Stroke canceled while counting = %v, want %v", err, context.Canceled)
	}
	if allocs > 2 {
		t.Errorf("Stroke canceled while counting made %v allocations, want at most 2", allocs)
	}
}

func BenchmarkStroke(b *testing.B) {
	line := Polyline{Points: lissajousPoints(3, 2, math.Pi/2, 4826), Closed: true}
	for i, p := range line.Points {
		line.Points[i] = Point{X: 512 + 460*p.X, Y: 512 - 460*p.Y}
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Stroke(b.Context(), line, 4); err != nil {
			b.Fatal(err)
		}
	}
}
