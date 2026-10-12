package fractal

import (
	"context"
	"image"
	"image/color"
	"math"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

// Work scheduling of the escape-time generators.
const (
	// blockRows is the height of a block of rows taken by one worker.
	blockRows = 16
	// checkEvery is the number of iterations between two ctx checks inside a block.
	checkEvery = 1 << 20
)

// escapeFunc iterates the point p of a pixel and returns the escape count and
// the last z, as escapeJulia does.
type escapeFunc func(p complex128) (int, complex128)

// escapeTime draws an escape-time fractal: the Mandelbrot and Julia sets
// differ only in escape. Inside points take the first palette color, escaped
// ones a smooth color by mu = n + 1 − log2(log|z|), normalized on a log scale.
type escapeTime struct {
	center  complex128
	width   float64
	height  float64
	maxIter int
	logMax  float64 // log(1 + maxIter)
	workers int
	palette *palette
	escape  escapeFunc
}

// newEscapeTime returns a generator of the area width × height around center;
// colors are copied into the palette table.
func newEscapeTime(
	center complex128, width, height float64, maxIter int, colors []config.Color, workers int, escape escapeFunc,
) escapeTime {
	return escapeTime{
		center:  center,
		width:   width,
		height:  height,
		maxIter: maxIter,
		logMax:  math.Log1p(float64(maxIter)),
		workers: workers,
		palette: newPalette(colors),
		escape:  escape,
	}
}

// generate draws an image of the given size. It returns a nil image and the
// error when ctx is done or the size is invalid.
func (e *escapeTime) generate(ctx context.Context, size figure.Size) (*image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := size.Validate(); err != nil {
		return nil, err
	}

	img := image.NewRGBA(image.Rect(0, 0, size.Width, size.Height))
	view := newViewport(e.center, e.width, e.height, size)
	err := figure.ForRows(ctx, size.Height, blockRows, e.workers, func(_, y0, y1 int) error {
		return e.rows(ctx, img, view, y0, y1)
	})
	if err != nil {
		return nil, err
	}
	return img, nil
}

// rows draws the rows [y0, y1) of img. It counts the iterations actually done
// and checks ctx every checkEvery of them, so a canceled ctx stops the block
// after at most checkEvery + maxIter iterations.
func (e *escapeTime) rows(ctx context.Context, img *image.RGBA, view viewport, y0, y1 int) error {
	width := img.Rect.Dx()
	budget := 0
	for y := y0; y < y1; y++ {
		pix := img.Pix[y*img.Stride : y*img.Stride+4*width]
		for x := range width {
			n, z := e.escape(view.At(x, y))
			c := e.color(n, z)
			i := 4 * x
			pix[i], pix[i+1], pix[i+2], pix[i+3] = c.R, c.G, c.B, c.A

			budget += n + 1
			if budget >= checkEvery {
				budget = 0
				if err := ctx.Err(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// color returns the color of a point with the escape count n and the last z.
func (e *escapeTime) color(n int, z complex128) color.RGBA {
	if n >= e.maxIter {
		return e.palette.table[0]
	}
	// |z| > 2 for an escaped point, so log|z| > 0 and log2 is defined.
	logZ := math.Log(abs2(z)) / 2
	mu := max(float64(n)+1-math.Log2(logZ), 0)
	return e.palette.At(math.Log1p(mu) / e.logMax)
}
