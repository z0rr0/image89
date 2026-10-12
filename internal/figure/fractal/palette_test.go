package fractal

import (
	"image/color"
	"math"
	"testing"

	"github.com/z0rr0/image89/internal/config"
)

func TestPaletteAt(t *testing.T) {
	t.Parallel()

	var (
		black = config.Color{}
		red   = config.Color{R: 0xff}
		green = config.Color{G: 0xff}
		blue  = config.Color{B: 0xff}
		white = config.Color{R: 0xff, G: 0xff, B: 0xff}
	)
	tests := []struct {
		name   string
		colors []config.Color
		t      float64
		want   color.RGBA
	}{
		{name: "start", colors: []config.Color{black, white}, t: 0, want: black.RGBA()},
		{name: "end", colors: []config.Color{black, white}, t: 1, want: white.RGBA()},
		{name: "middle", colors: []config.Color{black, white}, t: 0.5, want: color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}},
		{name: "below zero", colors: []config.Color{black, white}, t: -1, want: black.RGBA()},
		{name: "above one", colors: []config.Color{black, white}, t: 2, want: white.RGBA()},
		{name: "inner stop", colors: []config.Color{red, green, blue}, t: 0.5, want: green.RGBA()},
		{name: "last stop", colors: []config.Color{red, green, blue}, t: 1, want: blue.RGBA()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := newPalette(tt.colors).At(tt.t)
			if !near(got, tt.want, 1) {
				t.Errorf("At(%v) = %v, want %v ± 1", tt.t, got, tt.want)
			}
		})
	}
}

func TestPaletteNaN(t *testing.T) {
	t.Parallel()

	p := newPalette([]config.Color{{}, {R: 0xff, G: 0xff, B: 0xff}})
	got := p.At(math.NaN())
	if got != p.table[0] && got != p.table[paletteSize-1] {
		t.Errorf("At(NaN) = %v, want one of the ends", got)
	}
}

func TestPaletteKeepsNoSlice(t *testing.T) {
	t.Parallel()

	colors := []config.Color{{}, {R: 0xff}}
	p := newPalette(colors)
	colors[0] = config.Color{B: 0xff}
	if got := p.At(0); got != (color.RGBA{A: 0xff}) {
		t.Errorf("At(0) = %v after changing the source slice, want black", got)
	}
}

// near reports whether every channel of a and b differs by at most d.
func near(a, b color.RGBA, d int) bool {
	diff := func(x, y uint8) bool { return max(int(x)-int(y), int(y)-int(x)) <= d }
	return diff(a.R, b.R) && diff(a.G, b.G) && diff(a.B, b.B) && diff(a.A, b.A)
}
