package geometry

import (
	"context"
	"image"
	"image/color"

	"github.com/z0rr0/image89/internal/figure"
)

// look is the common part of the contour generators: colors, the padding and
// the line width as fractions of min(W, H), and the workers per image.
type look struct {
	color      color.RGBA
	background color.RGBA
	padding    float64
	lineWidth  float64
	workers    int
}

// start checks what every generator checks before any work: ctx is alive and
// the size is valid.
func start(ctx context.Context, size figure.Size) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return size.Validate()
}

// side returns min(W, H) of size in pixels.
func side(size figure.Size) float64 {
	return float64(min(size.Width, size.Height))
}

// paddingPx returns the padding in pixels.
func (l *look) paddingPx(size figure.Size) float64 {
	return l.padding * side(size)
}

// strokeWidth returns the line width in pixels, at least one pixel.
func (l *look) strokeWidth(size figure.Size) float64 {
	return max(1, l.lineWidth*side(size))
}

// strokeFit returns the transform of bbox into size that leaves room for the padding and the line.
func (l *look) strokeFit(bbox Rect, size figure.Size) Transform {
	return Fit(bbox, size, l.paddingPx(size), l.strokeWidth(size)/2)
}

// stroke draws line, given in math coordinates, as a line of strokeWidth
// fitted into size. It transforms line.Points in place.
func (l *look) stroke(ctx context.Context, size figure.Size, line Polyline) (*image.RGBA, error) {
	t := l.strokeFit(Bounds(line.Points), size)
	for i, p := range line.Points {
		line.Points[i] = t.Apply(p)
	}
	polys, err := Stroke(ctx, line, l.strokeWidth(size))
	if err != nil {
		return nil, err
	}
	return l.paint(ctx, size, polys)
}

// paint returns an image of size with polys, in pixel coordinates, painted
// in the color over the background; a nil image on an error. It checks ctx
// before allocating the image, so a request canceled while its geometry was
// built does not take 4·W·H bytes.
func (l *look) paint(ctx context.Context, size figure.Size, polys []Polygon) (*image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, size.Width, size.Height))
	FillBackground(img, l.background)
	if err := Fill(ctx, img, polys, l.color, l.workers); err != nil {
		return nil, err
	}
	return img, nil
}
