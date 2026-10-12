package fractal

import (
	"image"
	"strconv"
	"testing"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

// BenchmarkBlockRows draws every pixel fractal of the default preset as
// plane.generate does, but with blocks of other heights than blockRows.
func BenchmarkBlockRows(b *testing.B) {
	types := config.Default().Types
	planes := []struct {
		name  string
		plane plane
	}{
		{"mandelbrot", NewMandelbrot(types.Mandelbrot, 0).plane},
		{"julia", NewJulia(types.Julia, 0).plane},
		{"newton", NewNewton(types.Newton, 0).plane},
	}
	sizes := []figure.Size{{Width: 640, Height: 480}, {Width: 1920, Height: 1080}}

	for _, p := range planes {
		for _, size := range sizes {
			view := newViewport(p.plane.center, p.plane.width, p.plane.height, size)
			for _, rows := range []int{8, 16, 64} {
				b.Run(p.name+"/"+size.String()+"/"+strconv.Itoa(rows), func(b *testing.B) {
					ctx := b.Context()
					b.ReportAllocs()
					for b.Loop() {
						img := image.NewRGBA(image.Rect(0, 0, size.Width, size.Height))
						err := figure.ForRows(ctx, size.Height, rows, p.plane.workers, func(_, y0, y1 int) error {
							return p.plane.rows(ctx, img, view, y0, y1)
						})
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}
