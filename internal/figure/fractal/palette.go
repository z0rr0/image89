package fractal

import (
	"image/color"
	"math"

	"github.com/z0rr0/image89/internal/config"
)

// paletteSize is the number of precomputed gradient colors.
const paletteSize = 1024

// palette is a gradient through equally spaced colors, precomputed into a
// table, so a pixel takes one lookup.
type palette struct {
	table [paletteSize]color.RGBA
}

// newPalette builds the gradient through colors, which must hold at least two
// colors as the config validation guarantees. The slice is not kept.
func newPalette(colors []config.Color) *palette {
	p := &palette{}
	last := len(colors) - 1
	for i := range paletteSize {
		pos := float64(i) * float64(last) / (paletteSize - 1)
		k := min(int(pos), last-1)
		f := pos - float64(k)
		a, b := colors[k], colors[k+1]
		p.table[i] = color.RGBA{R: lerp(a.R, b.R, f), G: lerp(a.G, b.G, f), B: lerp(a.B, b.B, f), A: 0xff}
	}
	return p
}

// At returns the gradient color at t; t is clamped to [0, 1], and NaN gives
// one of the two ends.
func (p *palette) At(t float64) color.RGBA {
	i := int(t*(paletteSize-1) + 0.5)
	return p.table[min(max(i, 0), paletteSize-1)]
}

// lerp interpolates between a and b at f in [0, 1] with rounding.
func lerp(a, b uint8, f float64) uint8 {
	v := math.Round(float64(a) + (float64(b)-float64(a))*f)
	return uint8(int(v) & 0xff)
}
