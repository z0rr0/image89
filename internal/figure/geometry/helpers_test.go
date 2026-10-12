package geometry

import (
	"context"
	"image"
	"image/color"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

// white is the background of the test images.
func white() color.RGBA {
	return color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
}

// black is the color of the polygons in the test images.
func black() color.RGBA {
	return color.RGBA{A: 0xff}
}

// countdownCtx is never done by itself, but its Err starts to report
// context.Canceled after left calls, which stops the code at a known check
// without timing.
type countdownCtx struct {
	left atomic.Int64
}

func newCountdown(left int64) *countdownCtx {
	c := &countdownCtx{}
	c.left.Store(left)
	return c
}

func (*countdownCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*countdownCtx) Done() <-chan struct{}       { return nil }
func (*countdownCtx) Value(any) any               { return nil }

func (c *countdownCtx) Err() error {
	if c.left.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}

// rasterize returns a white image of w × h with polys filled in black by one worker.
func rasterize(t *testing.T, polys []Polygon, w, h int) *image.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	FillBackground(img, white())
	if err := Fill(t.Context(), img, polys, black(), 1); err != nil {
		t.Fatalf("Fill error: %v", err)
	}
	return img
}

// coverage returns the painted area of a black on white image in pixels: the
// sum of the coverage of every pixel.
func coverage(img *image.RGBA) float64 {
	var sum float64
	for i := 0; i < len(img.Pix); i += 4 {
		sum += float64(0xff-img.Pix[i]) / 0xff
	}
	return sum
}

// near reports whether a and b differ by at most tol in every color channel.
func near(a, b color.RGBA, tol int) bool {
	diff := func(x, y uint8) int { return max(int(x)-int(y), int(y)-int(x)) }
	return diff(a.R, b.R) <= tol && diff(a.G, b.G) <= tol && diff(a.B, b.B) <= tol && diff(a.A, b.A) <= tol
}

// at returns the pixel of img that contains the point p in pixel coordinates.
func at(img *image.RGBA, p Point) color.RGBA {
	return img.RGBAAt(int(math.Floor(p.X)), int(math.Floor(p.Y)))
}

// distToSegment returns the distance from p to the segment ab.
func distToSegment(p, a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	t := 0.0
	if l2 := dx*dx + dy*dy; l2 > 0 {
		t = min(1, max(0, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l2))
	}
	return math.Hypot(p.X-a.X-t*dx, p.Y-a.Y-t*dy)
}

// distToLine returns the distance from p to the polyline pl.
func distToLine(p Point, pl Polyline) float64 {
	d := math.Inf(1)
	n := len(pl.Points)
	for i := range n - 1 {
		d = min(d, distToSegment(p, pl.Points[i], pl.Points[i+1]))
	}
	if pl.Closed && n > 1 {
		d = min(d, distToSegment(p, pl.Points[n-1], pl.Points[0]))
	}
	return d
}

// inside reports whether p is inside any of polys by the even-odd rule.
func inside(polys []Polygon, p Point) bool {
	for _, poly := range polys {
		in := false
		prev := poly[len(poly)-1]
		for _, q := range poly {
			if (q.Y > p.Y) != (prev.Y > p.Y) && p.X < prev.X+(p.Y-prev.Y)*(q.X-prev.X)/(q.Y-prev.Y) {
				in = !in
			}
			prev = q
		}
		if in {
			return true
		}
	}
	return false
}
