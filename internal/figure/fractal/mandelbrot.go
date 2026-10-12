package fractal

import (
	"context"
	"image"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

// Mandelbrot draws the Mandelbrot set: the points c for which the orbit of
// z ← z² + c from z = 0 stays bounded.
type Mandelbrot struct {
	plane plane
}

// NewMandelbrot returns a generator of the preset p, which must be valid, that
// uses workers goroutines per image (0 means GOMAXPROCS). It copies what it
// needs from p, so later changes of p do not affect it.
//
// The points of the main cardioid and of the disk of period 2 are inside the
// set, so they get maxIter without iterating; the image is the same. Their
// maxIter still counts towards the ctx check budget, so the checks only
// come more often.
func NewMandelbrot(p config.Mandelbrot, workers int) *Mandelbrot {
	maxIter := p.Iterations
	escape := func(c complex128) (int, complex128) {
		if inMainBulbs(c) {
			return maxIter, c
		}
		return escapeMandelbrot(c, maxIter)
	}
	center := complex(p.Center[0], p.Center[1])
	return &Mandelbrot{plane: escapeTime(center, p.Width, p.Height, maxIter, p.Palette, workers, escape)}
}

// Generate draws an image of the given size. It returns a nil image and the
// error when ctx is done before the image is complete or the size is invalid.
func (m *Mandelbrot) Generate(ctx context.Context, size figure.Size) (*image.RGBA, error) {
	return m.plane.generate(ctx, size)
}
