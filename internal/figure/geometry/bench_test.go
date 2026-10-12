package geometry

import (
	"image"
	"strconv"
	"testing"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
)

// resolutionDepth is a preset depth above any depth an image allows, so the
// generators draw the deepest level the resolution allows.
const resolutionDepth = 64

// outline returns the polygons that look.stroke fills for line in an image
// of size: the line fitted into the image and stroked.
func outline(b *testing.B, l *look, size figure.Size, line Polyline) []Polygon {
	b.Helper()
	t := l.strokeFit(Bounds(line.Points), size)
	for i, p := range line.Points {
		line.Points[i] = t.Apply(p)
	}
	polys, err := Stroke(b.Context(), line, l.strokeWidth(size))
	if err != nil {
		b.Fatal(err)
	}
	return polys
}

// kochPolygons returns the polygons of the Koch snowflake of the preset p in
// an image of size.
func kochPolygons(b *testing.B, p config.Koch, size figure.Size) []Polygon {
	b.Helper()
	g := NewKoch(p, 0)
	return outline(b, &g.look, size, kochOutline(g.depthFor(size)))
}

// lissajousPolygons returns the polygons of the Lissajous curve of the preset
// p in an image of size.
func lissajousPolygons(b *testing.B, p config.Lissajous, size figure.Size) []Polygon {
	b.Helper()
	g := NewLissajous(p, 0)
	n := lissajousCount(g.points, g.a, g.b, size)
	return outline(b, &g.look, size, Polyline{Points: lissajousPoints(g.a, g.b, g.delta, n), Closed: true})
}

// mib returns the name of a size in MiB.
func mib(n int) string {
	return strconv.Itoa(n>>20) + "MiB"
}

// BenchmarkFillBands fills the default Koch snowflake and Lissajous curve
// with other band pixel budgets and scratch limits than bandPixels and
// scratchCap; rasters is the number of rasterizers created.
func BenchmarkFillBands(b *testing.B) {
	types := config.Default().Types
	sizes := []figure.Size{{Width: 4096, Height: 4096}, {Width: figure.MaxSide, Height: 256}}
	for _, size := range sizes {
		img := image.NewRGBA(image.Rect(0, 0, size.Width, size.Height))
		figures := []struct {
			name  string
			polys []Polygon
		}{
			{"koch", kochPolygons(b, types.Koch, size)},
			{"lissajous", lissajousPolygons(b, types.Lissajous, size)},
		}
		for _, f := range figures {
			for _, budget := range []int{1 << 19, 1 << 20, 1 << 21} {
				rows := min(maxBandRows, max(1, budget/size.Width))
				for _, scratch := range []int{16 << 20, 32 << 20, 64 << 20} {
					name := f.name + "/" + size.String() + "/" + strconv.Itoa(budget>>10) + "K/" + mib(scratch)
					b.Run(name, func(b *testing.B) {
						created := 0
						b.ReportAllocs()
						for b.Loop() {
							var err error
							if created, err = fill(b.Context(), img, f.polys, black(), 0, rows, scratch); err != nil {
								b.Fatal(err)
							}
						}
						b.ReportMetric(float64(created), "rasters")
					})
				}
			}
		}
	}
}

// BenchmarkStrokeKoch strokes the Koch snowflake of depth 7 in 4096×4096
// with the default and a thick line; points is the number of polygon points,
// so B/op shows how much the capacity estimate of Stroke overshoots.
func BenchmarkStrokeKoch(b *testing.B) {
	size := figure.Size{Width: 4096, Height: 4096}
	for _, lineWidth := range []float64{0.004, 0.02} {
		p := config.Default().Types.Koch
		p.Depth = 7
		p.LineWidth = lineWidth
		g := NewKoch(p, 0)
		line := kochOutline(g.depthFor(size))
		t := g.look.strokeFit(Bounds(line.Points), size)
		for i, pt := range line.Points {
			line.Points[i] = t.Apply(pt)
		}
		width := g.look.strokeWidth(size)
		b.Run(strconv.FormatFloat(lineWidth, 'g', -1, 64), func(b *testing.B) {
			points := 0
			b.ReportAllocs()
			for b.Loop() {
				polys, err := Stroke(b.Context(), line, width)
				if err != nil {
					b.Fatal(err)
				}
				points = 0
				for _, poly := range polys {
					points += len(poly)
				}
			}
			b.ReportMetric(float64(points), "points")
		})
	}
}

// BenchmarkGeometryAtMaxSide builds the geometry of the contour figures at
// the deepest level the resolution allows, without the image: the polygons
// and their bands, which come on top of the RGBA buffer of 4·W·H bytes.
func BenchmarkGeometryAtMaxSide(b *testing.B) {
	types := config.Default().Types
	for _, side := range []int{4096, figure.MaxSide} {
		size := figure.Size{Width: side, Height: side}
		rows := bandRows(size.Width)
		b.Run("sierpinski/"+size.String(), func(b *testing.B) {
			g := NewSierpinski(config.Sierpinski{Depth: resolutionDepth, Padding: types.Sierpinski.Padding}, 0)
			t := g.fit(size)
			depth := sierpinskiDepth(g.depth, t.Scale)
			b.ReportAllocs()
			for b.Loop() {
				tris := sierpinskiTriangles(depth)
				set := polygonSet{points: make([]Point, 0, 3*len(tris)), ends: make([]int, 0, len(tris))}
				for _, tri := range tris {
					set.add(t.Apply(tri[0]), t.Apply(tri[1]), t.Apply(tri[2]))
				}
				bucket(set.polygons(), 0, size.Height, rows)
			}
			b.ReportMetric(float64(depth), "depth")
		})
		b.Run("koch/"+size.String(), func(b *testing.B) {
			p := types.Koch
			p.Depth = resolutionDepth
			g := NewKoch(p, 0)
			depth := g.depthFor(size)
			b.ReportAllocs()
			for b.Loop() {
				bucket(outline(b, &g.look, size, kochOutline(depth)), 0, size.Height, rows)
			}
			b.ReportMetric(float64(depth), "depth")
		})
		b.Run("lissajous/"+size.String(), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				bucket(lissajousPolygons(b, types.Lissajous, size), 0, size.Height, rows)
			}
		})
	}
}
