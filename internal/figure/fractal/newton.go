package fractal

import (
	"context"
	"image"
	"image/color"
	"math"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

// newtonFade is the largest share of the way from the color of a root to the
// background: the more steps a point takes, the closer its color comes to the
// background, but a point that converged never takes the background color itself.
const newtonFade = 0.75

// Newton draws the Newton fractal of zⁿ − 1: every point is colored by the root
// that Newton's method converges to from it, darker (closer to the background)
// the more steps it takes. The visible area is centered at zero.
type Newton struct {
	plane plane
}

// NewNewton returns a generator of the preset p, which must be valid, that uses
// workers goroutines per image (0 means GOMAXPROCS). It copies what it needs
// from p, so later changes of p do not affect it.
func NewNewton(p config.Newton, workers int) *Newton {
	degree, maxIter := p.Degree, p.Iterations
	eps2 := p.Epsilon * p.Epsilon
	logMax := math.Log1p(float64(maxIter))
	background := p.Background.RGBA()
	roots := make([]*palette, degree)
	for k := range roots {
		roots[k] = newPalette([]config.Color{p.Colors[k], p.Background})
	}

	pixel := func(z complex128) (color.RGBA, int) {
		k, n := newtonRoot(z, degree, eps2, maxIter)
		if k < 0 {
			return background, n
		}
		return roots[k].At(newtonFade * math.Log1p(float64(n)) / logMax), n
	}
	return &Newton{plane: plane{width: p.Width, height: p.Height, workers: workers, pixel: pixel}}
}

// Generate draws an image of the given size. It returns a nil image and the
// error when ctx is done before the image is complete or the size is invalid.
func (g *Newton) Generate(ctx context.Context, size figure.Size) (*image.RGBA, error) {
	return g.plane.generate(ctx, size)
}
