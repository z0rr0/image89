package geometry

import (
	"context"
	"image"
	"math"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

// Bounds of the automatic number of points of the Lissajous curve.
const (
	minLissajousPoints = 256
	maxLissajousPoints = 65536
)

// Lissajous draws the closed Lissajous curve x = sin(a·t + δ), y = sin(b·t).
type Lissajous struct {
	look   look
	a, b   int
	delta  float64
	points int
}

// NewLissajous returns a generator of the preset p, which must be valid, that
// uses workers goroutines per image (0 means GOMAXPROCS).
func NewLissajous(p config.Lissajous, workers int) *Lissajous {
	return &Lissajous{
		look: look{
			color: p.Color.RGBA(), background: p.Background.RGBA(),
			padding: p.Padding, lineWidth: p.LineWidth, workers: workers,
		},
		a:      p.A,
		b:      p.B,
		delta:  p.Delta,
		points: p.Points,
	}
}

// Generate draws an image of the given size. It returns a nil image and the
// error when ctx is done before the image is complete or the size is invalid.
func (g *Lissajous) Generate(ctx context.Context, size figure.Size) (*image.RGBA, error) {
	if err := start(ctx, size); err != nil {
		return nil, err
	}
	n := lissajousCount(g.points, g.a, g.b, size)
	return g.look.stroke(ctx, size, Polyline{Points: lissajousPoints(g.a, g.b, g.delta, n), Closed: true})
}

// lissajousCount returns points if it is set. Otherwise it chooses the number
// of points from the image size, so that the chords of a curve with the radius
// R = min(W, H)/2 pixels are about 2 pixels long: ceil(π·R·max(a, b)) within
// [minLissajousPoints, maxLissajousPoints].
func lissajousCount(points, a, b int, size figure.Size) int {
	if points > 0 {
		return points
	}
	n := math.Ceil(math.Pi * side(size) / 2 * float64(max(a, b)))
	return int(min(max(n, minLissajousPoints), maxLissajousPoints))
}

// lissajousPoints returns n points of the curve x = sin(a·t + δ), y = sin(b·t)
// in math coordinates at t = T·i/n for i in [0, n). The curve closes after
// the period T = 2π/gcd(a, b), so the point of t = T, equal to the first one,
// is not included.
//
// x is computed as sin(a·t)·cos δ + cos(a·t)·sin δ: in the sum a·t + δ a large
// δ, which the config accepts, would absorb a·t and turn the curve into a line.
func lissajousPoints(a, b int, delta float64, n int) []Point {
	period := 2 * math.Pi / float64(gcd(a, b))
	fa, fb := float64(a), float64(b)
	sinDelta, cosDelta := math.Sincos(delta)
	pts := make([]Point, n)
	for i := range pts {
		t := period * float64(i) / float64(n)
		sin, cos := math.Sincos(fa * t)
		pts[i] = Point{X: sin*cosDelta + cos*sinDelta, Y: math.Sin(fb * t)}
	}
	return pts
}

// gcd returns the greatest common divisor of a and b.
func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
