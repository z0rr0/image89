package fractal

import (
	"context"
	"image"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

// Julia draws the filled Julia set of a fixed c: the points z whose orbit of
// z ← z² + c stays bounded. The visible area is centered at zero.
type Julia struct {
	plane plane
}

// NewJulia returns a generator of the preset p, which must be valid, that uses
// workers goroutines per image (0 means GOMAXPROCS). It copies what it needs
// from p, so later changes of p do not affect it. The escape radius is
// max(2, |c|): an orbit outside it never comes back for any c.
func NewJulia(p config.Julia, workers int) *Julia {
	maxIter := p.Iterations
	c := complex(p.C[0], p.C[1])
	r2 := max(mandelbrotRadius2, abs2(c))
	escape := func(z complex128) (int, complex128) { return escapeJulia(z, c, r2, maxIter) }
	return &Julia{plane: escapeTime(0, p.Width, p.Height, maxIter, p.Palette, workers, escape)}
}

// Generate draws an image of the given size. It returns a nil image and the
// error when ctx is done before the image is complete or the size is invalid.
func (j *Julia) Generate(ctx context.Context, size figure.Size) (*image.RGBA, error) {
	return j.plane.generate(ctx, size)
}
