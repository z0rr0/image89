package fractal

import (
	"image/color"
	"math"

	"github.com/z0rr0/image89/internal/config"
)

// escapeFunc iterates the point p of a pixel and returns the escape count and
// the last z, as escapeJulia does.
type escapeFunc func(p complex128) (int, complex128)

// escapeTime returns the plane of an escape-time fractal of the area
// width × height around center: the Mandelbrot and Julia sets differ only in
// escape. Colors are copied into the palette table.
func escapeTime(
	center complex128, width, height float64, maxIter int, colors []config.Color, workers int, escape escapeFunc,
) plane {
	s := newSmooth(maxIter, colors)
	return plane{
		center:  center,
		width:   width,
		height:  height,
		workers: workers,
		pixel: func(p complex128) (color.RGBA, int) {
			n, z := escape(p)
			return s.color(n, z), n
		},
	}
}

// smooth colors the points of an escape-time fractal. Inside points take the
// first palette color, escaped ones a smooth color by mu = n + 1 − log2(log|z|),
// normalized on a log scale.
type smooth struct {
	maxIter int
	logMax  float64 // log(1 + maxIter)
	palette *palette
}

// newSmooth returns the coloring of maxIter iterations through colors.
func newSmooth(maxIter int, colors []config.Color) *smooth {
	return &smooth{maxIter: maxIter, logMax: math.Log1p(float64(maxIter)), palette: newPalette(colors)}
}

// color returns the color of a point with the escape count n and the last z.
func (s *smooth) color(n int, z complex128) color.RGBA {
	if n >= s.maxIter {
		return s.palette.table[0]
	}
	// |z| > 2 for an escaped point, so log|z| > 0 and log2 is defined.
	logZ := math.Log(abs2(z)) / 2
	mu := max(float64(n)+1-math.Log2(logZ), 0)
	return s.palette.At(math.Log1p(mu) / s.logMax)
}
