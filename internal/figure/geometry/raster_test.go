package geometry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"runtime"
	"testing"
)

func TestFillBackground(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 7, 5))
	c := white()
	c.R = 0x12
	FillBackground(img, c)
	for y := range 5 {
		for x := range 7 {
			if got := img.RGBAAt(x, y); got != c {
				t.Fatalf("pixel (%d, %d) = %v, want %v", x, y, got, c)
			}
		}
	}
}

func TestFillRectangle(t *testing.T) {
	t.Parallel()

	// A line 100 px long and 4 px wide.
	rect := Polygon{{X: 50, Y: 48}, {X: 150, Y: 48}, {X: 150, Y: 52}, {X: 50, Y: 52}}
	if got := coverage(rasterize(t, []Polygon{rect}, 200, 100)); math.Abs(got-400) > 20 {
		t.Errorf("painted area = %.1f, want 400 ± 20", got)
	}
}

// bandShape returns polygons with long slanted edges across many bands and
// joins between them: the stroke of a zigzag over the whole image.
func bandShape(t *testing.T, w, h int) []Polygon {
	t.Helper()
	pts := make([]Point, 0, 16)
	for i := range 16 {
		pts = append(pts, Point{X: float64(w) * (0.05 + 0.06*float64(i)), Y: float64(h) * (0.1 + 0.8*float64(i%2))})
	}
	polys, err := Stroke(t.Context(), Polyline{Points: pts}, 7.3)
	if err != nil {
		t.Fatalf("Stroke error: %v", err)
	}
	return polys
}

// rowCoverage returns the painted area of row y of a black on white image.
func rowCoverage(img *image.RGBA, y int) float64 {
	var sum float64
	row := img.Pix[y*img.Stride : y*img.Stride+4*img.Rect.Dx()]
	for i := 0; i < len(row); i += 4 {
		sum += float64(0xff-row[i]) / 0xff
	}
	return sum
}

// TestFillBands compares thin and tall bands. The rasterizer carries the
// rounding residue of every row into the next one until the end of its mask,
// so bands of another height change anti-aliased pixels by one level here and
// there in every row. A seam would lose or double the coverage of a row
// instead, so the test compares the painted area of every row. Images up to
// 512 px use the fixed point rasterizer, wider ones the floating point one.
func TestFillBands(t *testing.T) {
	t.Parallel()

	for _, size := range []image.Point{{X: 256, Y: 256}, {X: 600, Y: 300}} {
		t.Run(fmt.Sprintf("%dx%d", size.X, size.Y), func(t *testing.T) {
			t.Parallel()

			polys := bandShape(t, size.X, size.Y)
			images := make([]*image.RGBA, 2)
			for i, rows := range []int{8, maxBandRows} {
				images[i] = image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
				FillBackground(images[i], white())
				if _, err := fill(t.Context(), images[i], polys, black(), 2, rows, scratchCap); err != nil {
					t.Fatalf("fill error: %v", err)
				}
			}
			thin, tall := images[0], images[1]
			for y := range size.Y {
				for x := range size.X {
					if a, b := thin.RGBAAt(x, y), tall.RGBAAt(x, y); !near(a, b, 1) {
						t.Fatalf("pixel (%d, %d) is %v with 8-row bands and %v with %d-row bands", x, y, a, b, maxBandRows)
					}
				}
				if a, b := rowCoverage(thin, y), rowCoverage(tall, y); math.Abs(a-b) > 0.5 {
					t.Errorf("row %d has the painted area %.2f with 8-row bands and %.2f with %d-row bands", y, a, b, maxBandRows)
				}
			}
		})
	}
}

// TestFillNoSeams fills a large square turned by 30° across many thin bands:
// every pixel entirely inside it is painted fully and every pixel entirely
// outside is untouched, also on the first and last rows of the bands.
func TestFillNoSeams(t *testing.T) {
	t.Parallel()

	for _, size := range []image.Point{{X: 256, Y: 256}, {X: 600, Y: 300}} {
		t.Run(fmt.Sprintf("%dx%d", size.X, size.Y), func(t *testing.T) {
			t.Parallel()

			c := Point{X: float64(size.X) / 2, Y: float64(size.Y) / 2}
			r := 0.45 * float64(min(size.X, size.Y))
			square := make(Polygon, 4)
			for i := range square {
				a := math.Pi/6 + float64(i)*math.Pi/2
				square[i] = Point{X: c.X + r*math.Cos(a), Y: c.Y + r*math.Sin(a)}
			}
			img := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
			FillBackground(img, white())
			if _, err := fill(t.Context(), img, []Polygon{square}, black(), 4, 8, scratchCap); err != nil {
				t.Fatalf("fill error: %v", err)
			}
			outline := Polyline{Points: square, Closed: true}
			for y := range size.Y {
				for x := range size.X {
					p := Point{X: float64(x) + 0.5, Y: float64(y) + 0.5}
					if distToLine(p, outline) < 0.75 {
						continue // a pixel on the edge
					}
					want := white()
					if inside([]Polygon{square}, p) {
						want = black()
					}
					if got := img.RGBAAt(x, y); got != want {
						t.Fatalf("pixel (%d, %d) = %v, want %v", x, y, got, want)
					}
				}
			}
		})
	}
}

func TestFillWorkers(t *testing.T) {
	t.Parallel()

	polys := bandShape(t, 600, 300)
	var want []byte
	for _, workers := range []int{1, 4, 0} {
		img := image.NewRGBA(image.Rect(0, 0, 600, 300))
		FillBackground(img, white())
		if err := Fill(t.Context(), img, polys, black(), workers); err != nil {
			t.Fatalf("Fill error: %v", err)
		}
		if want == nil {
			want = img.Pix
		} else if !bytes.Equal(img.Pix, want) {
			t.Errorf("%d workers give another image than 1 worker", workers)
		}
	}
}

func TestBandRows(t *testing.T) {
	t.Parallel()

	for width, want := range map[int]int{1: maxBandRows, 200: maxBandRows, 8192: 128, 8193: 127, 65536: 16, 1 << 21: 1} {
		if got := bandRows(width); got != want {
			t.Errorf("bandRows(%d) = %d, want %d", width, got, want)
		}
	}
}

func TestRasterWorkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                         string
		width, height, rows, workers int
		scratch                      int
		want                         int
	}{
		{name: "scratch limit", width: 65536, height: 256, rows: 16, workers: 16, scratch: scratchCap, want: 4},
		{name: "workers limit", width: 1024, height: 1024, rows: 128, workers: 3, scratch: scratchCap, want: 3},
		{name: "bands limit", width: 200, height: 100, rows: 128, workers: 8, scratch: scratchCap, want: 1},
		{name: "4096 square", width: 4096, height: 4096, rows: 128, workers: 16, scratch: scratchCap, want: 8},
		{name: "at least one", width: 65536, height: 256, rows: 16, workers: 16, scratch: 1, want: 1},
	}
	for _, tt := range tests {
		if got := rasterWorkers(tt.width, tt.height, tt.rows, tt.workers, tt.scratch); got != tt.want {
			t.Errorf("%s: rasterWorkers = %d, want %d", tt.name, got, tt.want)
		}
	}
	if got := rasterWorkers(1024, 1024, 8, 0, scratchCap); got < 1 || got > runtime.GOMAXPROCS(0) {
		t.Errorf("rasterWorkers with 0 workers = %d, want 1..GOMAXPROCS", got)
	}
}

func TestFillRasterizers(t *testing.T) {
	t.Parallel()

	const w, h, rows = 256, 64, 8
	all := bandShape(t, w, h)
	top := []Polygon{{{X: 10, Y: 1}, {X: 100, Y: 1}, {X: 100, Y: 5}}}
	tests := []struct {
		name    string
		polys   []Polygon
		scratch int
		want    int
	}{
		// 8 bands, 8 workers, masks for two of them.
		{name: "scratch", polys: all, scratch: 2 * maskBytes * w * rows, want: 2},
		// Only the first band has polygons; the others are skipped without a rasterizer.
		{name: "empty bands", polys: top, scratch: scratchCap, want: 1},
	}
	for _, tt := range tests {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		created, err := fill(t.Context(), img, tt.polys, black(), 8, rows, tt.scratch)
		if err != nil {
			t.Fatalf("%s: fill error: %v", tt.name, err)
		}
		if created < 1 || created > tt.want {
			t.Errorf("%s: created %d rasterizers, want 1..%d", tt.name, created, tt.want)
		}
	}
}

func TestFillSkips(t *testing.T) {
	t.Parallel()

	nan := math.NaN()
	polys := []Polygon{
		{{X: 10, Y: 10}, {X: 20, Y: 10}},                   // too few points
		{{X: 10, Y: 10}, {X: 20, Y: 10}, {X: 30, Y: 10}},   // no height
		{{X: 10, Y: -20}, {X: 20, Y: -20}, {X: 20, Y: -5}}, // above the image
		{{X: 10, Y: 60}, {X: 20, Y: 60}, {X: 20, Y: 70}},   // below the image
		{{X: 10, Y: nan}, {X: 20, Y: 10}, {X: 20, Y: 30}},  // not a number
	}
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	FillBackground(img, white())
	created, err := fill(t.Context(), img, polys, black(), 1, 8, scratchCap)
	if err != nil || created != 0 {
		t.Errorf("fill = %d rasterizers, %v; want none", created, err)
	}
	if coverage(img) != 0 {
		t.Error("skipped polygons painted the image")
	}
	if created, err := fill(t.Context(), img, nil, black(), 1, 8, scratchCap); err != nil || created != 0 {
		t.Errorf("fill without polygons = %d rasterizers, %v; want none", created, err)
	}
}

// TestFillOffset fills an image whose bounds do not start at zero: polygons
// are in the coordinates of the image.
func TestFillOffset(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(100, 200, 300, 300))
	FillBackground(img, white())
	rect := Polygon{{X: 150, Y: 248}, {X: 250, Y: 248}, {X: 250, Y: 252}, {X: 150, Y: 252}}
	if err := Fill(t.Context(), img, []Polygon{rect}, black(), 1); err != nil {
		t.Fatalf("Fill error: %v", err)
	}
	if got := coverage(img); math.Abs(got-400) > 20 {
		t.Errorf("painted area = %.1f, want 400 ± 20", got)
	}
	if c := img.RGBAAt(200, 250); c != black() {
		t.Errorf("pixel inside = %v, want black()", c)
	}
}

func TestFillCanceled(t *testing.T) {
	t.Parallel()

	polys := bandShape(t, 200, 100)
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	FillBackground(img, white())

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Fill(ctx, img, polys, black(), 1); !errors.Is(err, context.Canceled) {
		t.Errorf("Fill with a canceled ctx = %v, want %v", err, context.Canceled)
	}
	if coverage(img) != 0 {
		t.Error("Fill with a canceled ctx painted the image")
	}

	// Many polygons in one band: fill and ForRows check ctx once each before
	// the band, the check inside the band stops it.
	many := make([]Polygon, 2*fillCheckEvery)
	for i := range many {
		x := float64(i % 190)
		many[i] = Polygon{{X: x, Y: 1}, {X: x + 5, Y: 1}, {X: x + 5, Y: 5}}
	}
	if _, err := fill(newCountdown(2), img, many, black(), 1, 8, scratchCap); !errors.Is(err, context.Canceled) {
		t.Errorf("fill canceled inside a band = %v, want %v", err, context.Canceled)
	}
}

func BenchmarkFill(b *testing.B) {
	const side = 1024
	// 10 000 small squares on a grid of 100 × 100.
	polys := make([]Polygon, 0, 10000)
	for i := range 100 {
		for j := range 100 {
			x, y := 2+10.2*float64(i), 2+10.2*float64(j)
			polys = append(polys, Polygon{{X: x, Y: y}, {X: x + 6.5, Y: y}, {X: x + 6.5, Y: y + 6.5}, {X: x, Y: y + 6.5}})
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	b.ReportAllocs()
	for b.Loop() {
		if err := Fill(b.Context(), img, polys, black(), 0); err != nil {
			b.Fatal(err)
		}
	}
}
