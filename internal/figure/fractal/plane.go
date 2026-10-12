package fractal

import (
	"context"
	"image"
	"image/color"

	"github.com/z0rr0/image89/internal/figure"
)

// Work scheduling of the pixel fractals.
const (
	// blockRows is the height of a block of rows taken by one worker.
	blockRows = 16
	// checkEvery is the number of iterations between two ctx checks inside a block.
	checkEvery = 1 << 20
)

// pixelFunc returns the color of the point p of a pixel and the number of
// iterations spent on it, which counts towards the ctx check budget.
type pixelFunc func(p complex128) (color.RGBA, int)

// plane draws a pixel fractal: every pixel is the point of the visible area
// width × height around center at the pixel center, colored by pixel. All
// pixel fractals share it.
type plane struct {
	center  complex128
	width   float64
	height  float64
	workers int
	pixel   pixelFunc
}

// generate draws an image of the given size. It returns a nil image and the
// error when ctx is done or the size is invalid.
func (p *plane) generate(ctx context.Context, size figure.Size) (*image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := size.Validate(); err != nil {
		return nil, err
	}

	img := image.NewRGBA(image.Rect(0, 0, size.Width, size.Height))
	view := newViewport(p.center, p.width, p.height, size)
	err := figure.ForRows(ctx, size.Height, blockRows, p.workers, func(_, y0, y1 int) error {
		return p.rows(ctx, img, view, y0, y1)
	})
	if err != nil {
		return nil, err
	}
	return img, nil
}

// rows draws the rows [y0, y1) of img. It counts the iterations actually done
// and checks ctx every checkEvery of them, so a canceled ctx stops the block
// after at most checkEvery iterations plus the iterations of one pixel.
func (p *plane) rows(ctx context.Context, img *image.RGBA, view viewport, y0, y1 int) error {
	width := img.Rect.Dx()
	budget := 0
	for y := y0; y < y1; y++ {
		pix := img.Pix[y*img.Stride : y*img.Stride+4*width]
		for x := range width {
			c, n := p.pixel(view.At(x, y))
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
