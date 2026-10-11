package config

import (
	"math"
)

// Preset bounds.
const (
	// maxFraction caps line widths and paddings, which are fractions of min(W, H).
	maxFraction      = 0.25
	minPaletteColors = 2
	minNewtonDegree  = 3
	maxNewtonDegree  = 8
	maxLissajousFreq = 32
	minLissajousPts  = 16
	maxLissajousPts  = 65536
)

// Types holds the fixed preset of every figure.
type Types struct {
	Mandelbrot Mandelbrot `toml:"mandelbrot"`
	Julia      Julia      `toml:"julia"`
	Newton     Newton     `toml:"newton"`
	Sierpinski Sierpinski `toml:"sierpinski"`
	Koch       Koch       `toml:"koch"`
	Lissajous  Lissajous  `toml:"lissajous"`
}

// Mandelbrot is the preset of the Mandelbrot set.
// Width and Height set the visible area of the complex plane; the image shows
// the whole area at any aspect ratio, with equal steps along both axes.
type Mandelbrot struct {
	Iterations int `toml:"iterations"`
	// Center is the center of the visible area as [re, im].
	Center []float64 `toml:"center"`
	Width  float64   `toml:"width"`
	Height float64   `toml:"height"`
	// Palette is a gradient for escaped points; its first color also paints the set itself.
	Palette []Color `toml:"palette"`
}

// Julia is the preset of the Julia set; the visible area is centered at zero.
type Julia struct {
	Iterations int `toml:"iterations"`
	// C is the constant of z² + c as [re, im].
	C       []float64 `toml:"c"`
	Width   float64   `toml:"width"`
	Height  float64   `toml:"height"`
	Palette []Color   `toml:"palette"`
}

// Newton is the preset of the Newton fractal of zⁿ − 1; the visible area is centered at zero.
type Newton struct {
	Iterations int     `toml:"iterations"`
	Degree     int     `toml:"degree"`
	Width      float64 `toml:"width"`
	Height     float64 `toml:"height"`
	// Epsilon is the convergence tolerance.
	Epsilon float64 `toml:"epsilon"`
	// Colors paints the basin of each root; it needs at least Degree colors.
	Colors []Color `toml:"colors"`
	// Background paints points that converge to no root.
	Background Color `toml:"background"`
}

// Sierpinski is the preset of the Sierpinski triangle.
type Sierpinski struct {
	Depth int `toml:"depth"`
	// Padding is a fraction of min(W, H).
	Padding    float64 `toml:"padding"`
	Color      Color   `toml:"color"`
	Background Color   `toml:"background"`
}

// Koch is the preset of the Koch snowflake.
type Koch struct {
	Depth int `toml:"depth"`
	// LineWidth and Padding are fractions of min(W, H).
	LineWidth  float64 `toml:"line_width"`
	Padding    float64 `toml:"padding"`
	Color      Color   `toml:"color"`
	Background Color   `toml:"background"`
}

// Lissajous is the preset of the Lissajous curve x = sin(a·t + δ), y = sin(b·t).
type Lissajous struct {
	A     int     `toml:"a"`
	B     int     `toml:"b"`
	Delta float64 `toml:"delta"`
	// Points is the number of curve points; 0 chooses it from the image size.
	Points int `toml:"points"`
	// LineWidth and Padding are fractions of min(W, H).
	LineWidth  float64 `toml:"line_width"`
	Padding    float64 `toml:"padding"`
	Color      Color   `toml:"color"`
	Background Color   `toml:"background"`
}

// defaultTypes returns the default presets with newly allocated slices.
func defaultTypes() Types {
	var (
		black     = Color{0x00, 0x00, 0x00}
		white     = Color{0xff, 0xff, 0xff}
		navy      = Color{0x1d, 0x35, 0x57}
		crimson   = Color{0xe6, 0x39, 0x46}
		lineWidth = 0.004
		padding   = 0.05
	)
	return Types{
		Mandelbrot: Mandelbrot{
			Iterations: 256,
			Center:     []float64{-0.75, 0},
			Width:      3.5,
			Height:     2.5,
			Palette:    []Color{black, {0x00, 0x07, 0x64}, {0x20, 0x6b, 0xcb}, {0xed, 0xff, 0xff}, {0xff, 0xaa, 0x00}},
		},
		Julia: Julia{
			Iterations: 256,
			C:          []float64{-0.8, 0.156},
			Width:      3.2,
			Height:     2.4,
			Palette:    []Color{black, {0x1a, 0x0b, 0x3d}, {0x7b, 0x2c, 0xbf}, {0xff, 0x6d, 0x00}, white},
		},
		Newton: Newton{
			Iterations: 64,
			Degree:     3,
			Width:      4,
			Height:     4,
			Epsilon:    1e-6,
			Colors: []Color{
				crimson, {0x2a, 0x9d, 0x8f}, {0x45, 0x7b, 0x9d}, {0xf4, 0xa2, 0x61},
				{0x83, 0x38, 0xec}, {0x06, 0xd6, 0xa0}, {0xff, 0xbe, 0x0b}, {0xff, 0x00, 0x6e},
			},
			Background: black,
		},
		Sierpinski: Sierpinski{Depth: 7, Padding: padding, Color: navy, Background: white},
		Koch:       Koch{Depth: 5, LineWidth: lineWidth, Padding: padding, Color: navy, Background: white},
		Lissajous: Lissajous{
			A:          3,
			B:          2,
			Delta:      math.Pi / 2,
			Points:     0,
			LineWidth:  lineWidth,
			Padding:    padding,
			Color:      crimson,
			Background: white,
		},
	}
}

// validate checks every preset; iterations and depths are bounded by limits.
func (t *Types) validate(limits *Limits) error {
	var v validator
	t.Mandelbrot.validate(&v, limits)
	t.Julia.validate(&v, limits)
	t.Newton.validate(&v, limits)
	t.Sierpinski.validate(&v, limits)
	t.Koch.validate(&v, limits)
	t.Lissajous.validate(&v)
	return v.err()
}

func (p *Mandelbrot) validate(v *validator, limits *Limits) {
	checkRange(v, "types.mandelbrot.iterations", p.Iterations, 1, limits.MaxIterations)
	v.point("types.mandelbrot.center", p.Center)
	v.positive("types.mandelbrot.width", p.Width)
	v.positive("types.mandelbrot.height", p.Height)
	v.colors("types.mandelbrot.palette", p.Palette, minPaletteColors)
}

func (p *Julia) validate(v *validator, limits *Limits) {
	checkRange(v, "types.julia.iterations", p.Iterations, 1, limits.MaxIterations)
	v.point("types.julia.c", p.C)
	v.positive("types.julia.width", p.Width)
	v.positive("types.julia.height", p.Height)
	v.colors("types.julia.palette", p.Palette, minPaletteColors)
}

func (p *Newton) validate(v *validator, limits *Limits) {
	checkRange(v, "types.newton.iterations", p.Iterations, 1, limits.MaxIterations)
	checkRange(v, "types.newton.degree", p.Degree, minNewtonDegree, maxNewtonDegree)
	v.positive("types.newton.width", p.Width)
	v.positive("types.newton.height", p.Height)
	v.positive("types.newton.epsilon", p.Epsilon)
	v.colors("types.newton.colors", p.Colors, max(p.Degree, minNewtonDegree))
}

func (p *Sierpinski) validate(v *validator, limits *Limits) {
	checkRange(v, "types.sierpinski.depth", p.Depth, 0, limits.MaxDepth)
	v.fraction("types.sierpinski.padding", p.Padding)
}

func (p *Koch) validate(v *validator, limits *Limits) {
	checkRange(v, "types.koch.depth", p.Depth, 0, limits.MaxDepth)
	v.fraction("types.koch.line_width", p.LineWidth)
	v.fraction("types.koch.padding", p.Padding)
}

func (p *Lissajous) validate(v *validator) {
	checkRange(v, "types.lissajous.a", p.A, 1, maxLissajousFreq)
	checkRange(v, "types.lissajous.b", p.B, 1, maxLissajousFreq)
	v.finite("types.lissajous.delta", p.Delta)
	if p.Points != 0 {
		checkRange(v, "types.lissajous.points", p.Points, minLissajousPts, maxLissajousPts)
	}
	v.fraction("types.lissajous.line_width", p.LineWidth)
	v.fraction("types.lissajous.padding", p.Padding)
}
