package geometry

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"
	"runtime"

	"golang.org/x/image/vector"

	"github.com/z0rr0/image89/internal/figure"
)

// Band rasterization limits, chosen by measurements (docs/benchmarks.md).
const (
	// bandPixels bounds the pixels of one band. Images wider than
	// bandPixels/maxBandRows get lower bands, so scratchCap holds the masks of
	// at least scratchCap/(maskBytes·bandPixels) = 8 rasterizers at any width.
	bandPixels = 1 << 19
	// maxBandRows bounds the height of one band.
	maxBandRows = 128
	// scratchCap bounds the masks of all rasterizers of one Fill call.
	scratchCap = 32 << 20
	// maskBytes is the size of the mask per pixel: a rasterizer wider or
	// higher than 512 pixels keeps float32 and uint32 buffers.
	maskBytes = 8
	// fillCheckEvery is the number of polygons between two ctx checks in a band.
	fillCheckEvery = 4096
)

// FillBackground paints the whole image with c.
func FillBackground(img *image.RGBA, c color.RGBA) {
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
}

// Fill paints polys, given in pixel coordinates of img, with c over img, with
// anti-aliasing. Overlapping polygons are covered once: the coverage is
// clamped, so all polygons must have the same orientation (Stroke and the
// figures of this package give them a positive signed area).
//
// The image is rasterized in bands of whole rows, bandRows high, by up to
// workers goroutines (0 means GOMAXPROCS); the masks of all rasterizers take
// at most about scratchCap bytes. ctx is checked before every band and every
// fillCheckEvery polygons inside a band.
func Fill(ctx context.Context, img *image.RGBA, polys []Polygon, c color.RGBA, workers int) error {
	_, err := fill(ctx, img, polys, c, workers, bandRows(img.Rect.Dx()), scratchCap)
	return err
}

// bandRows returns the height of a band for an image of the given width.
func bandRows(width int) int {
	return min(maxBandRows, max(1, bandPixels/max(width, 1)))
}

// rasterWorkers returns the number of rasterizers for an image of width ×
// height in bands of rows: at most workers (0 means GOMAXPROCS), at most one
// per band and as many as fit into scratch bytes of masks, but at least one.
func rasterWorkers(width, height, rows, workers, scratch int) int {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	bands := 1 + (max(height, 1)-1)/rows
	return max(1, min(workers, bands, scratch/(maskBytes*width*rows)))
}

// fill is Fill with the band height and the scratch limit as parameters for
// tests. It returns the number of rasterizers it created.
func fill(ctx context.Context, img *image.RGBA, polys []Polygon, c color.RGBA, workers, rows, scratch int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	b := img.Rect
	width, height := b.Dx(), b.Dy()
	if width <= 0 || height <= 0 || len(polys) == 0 {
		return 0, nil
	}
	bands := bucket(polys, float64(b.Min.Y), height, rows)

	// The number of workers is normalized once: the slice of rasterizers and
	// ForRows must agree on it (see figure.ForRows).
	n := rasterWorkers(width, height, rows, workers, scratch)
	rasters := make([]*vector.Rasterizer, n)
	src := image.NewUniform(c)
	left := float32(b.Min.X)

	err := figure.ForRows(ctx, height, rows, n, func(worker, y0, y1 int) error {
		ids := bands[y0/rows]
		if len(ids) == 0 {
			return nil
		}
		z := rasters[worker]
		if z == nil {
			z = vector.NewRasterizer(width, y1-y0)
			rasters[worker] = z
		} else {
			// Draw consumes the mask, so every band starts from a clean one.
			z.Reset(width, y1-y0)
		}

		// The shift is subtracted after the conversion to float32: the
		// difference of a float32 and an integer is exact, so every band sees
		// the same rounded coordinates, only moved by whole rows.
		top := float32(b.Min.Y + y0)
		for j, id := range ids {
			if j%fillCheckEvery == fillCheckEvery-1 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			poly := polys[id]
			z.MoveTo(float32(poly[0].X)-left, float32(poly[0].Y)-top)
			for _, p := range poly[1:] {
				z.LineTo(float32(p.X)-left, float32(p.Y)-top)
			}
			z.ClosePath()
		}
		// The mask is indexed from the corner of the rectangle, which has
		// exactly the size of the mask, so no SubImage is needed.
		z.Draw(img, image.Rect(b.Min.X, b.Min.Y+y0, b.Max.X, b.Min.Y+y1), src, image.Point{})
		return nil
	})

	created := 0
	for _, z := range rasters {
		if z != nil {
			created++
		}
	}
	return created, err
}

// bucket returns, for every band of rows of an image of the given height whose
// first row is minY, the indices of the polygons that reach the band.
// Polygons without height, outside the image or with NaN coordinates are skipped.
func bucket(polys []Polygon, minY float64, height, rows int) [][]int {
	bands := make([][]int, 1+(height-1)/rows)
	for i, poly := range polys {
		if len(poly) < 3 {
			continue
		}
		lo, hi := poly[0].Y, poly[0].Y
		for _, p := range poly[1:] {
			lo, hi = min(lo, p.Y), max(hi, p.Y)
		}
		lo, hi = lo-minY, hi-minY
		if !(lo < hi) || hi <= 0 || lo >= float64(height) {
			continue
		}
		first := int(max(lo, 0)) / rows
		last := (int(math.Ceil(min(hi, float64(height)))) - 1) / rows
		for k := first; k <= last; k++ {
			bands[k] = append(bands[k], i)
		}
	}
	return bands
}
