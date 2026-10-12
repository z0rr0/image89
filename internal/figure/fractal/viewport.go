// Package fractal draws the pixel fractals of Image89: every pixel is a point
// of the complex plane colored by how its iteration behaves.
package fractal

import (
	"github.com/z0rr0/image89/internal/figure"
)

// viewport maps the pixels of an image to the complex plane. The visible area
// fits the image whole at any aspect ratio, with the same step along both
// axes, so the figure is never stretched; the imaginary axis points up.
type viewport struct {
	center     complex128
	halfWidth  float64 // image width / 2 in pixels
	halfHeight float64 // image height / 2 in pixels
	step       float64 // distance between neighbor pixels
}

// newViewport returns the viewport of the area width × height around center
// for an image of the given size: step = max(width/W, height/H).
func newViewport(center complex128, width, height float64, size figure.Size) viewport {
	w, h := float64(size.Width), float64(size.Height)
	return viewport{
		center:     center,
		halfWidth:  w / 2,
		halfHeight: h / 2,
		step:       max(width/w, height/h),
	}
}

// At returns the point at the center of the pixel (x, y). For even sides the
// center of the area lies between four pixels. The explicit float64
// conversions keep the compiler from fusing a multiply and an add, so the
// result is the same on every architecture.
func (v viewport) At(x, y int) complex128 {
	re := real(v.center) + float64((float64(x)+0.5-v.halfWidth)*v.step)
	im := imag(v.center) + float64((v.halfHeight-float64(y)-0.5)*v.step)
	return complex(re, im)
}
