# Figures

This page explains how Image89 draws each figure and how the image size affects the detail.
For the history and formulas of a figure, open `/<type>/info/` on the server. The presets of the figures
are in the `[types.<type>]` sections of [config.toml](config.toml).

| Type         | Figure                                         | How Image89 draws it                                                         |
|--------------|------------------------------------------------|------------------------------------------------------------------------------|
| `mandelbrot` | Mandelbrot set                                 | Each pixel is a point of the complex plane. The color shows its escape time. |
| `julia`      | Julia set of z² + c                            | The same, but the preset sets `c`. The pixel is the start value.             |
| `newton`     | Newton fractal of zⁿ − 1                       | The color shows the root that Newton's method reaches from the pixel.        |
| `sierpinski` | Sierpiński triangle                            | 3^d filled triangles, where d is the depth.                                  |
| `koch`       | Koch snowflake                                 | A closed line of 3·4^d segments.                                             |
| `lissajous`  | Lissajous curve x = sin(a·t + δ), y = sin(b·t) | A closed line through points of the curve.                                   |

## Pixel fractals

`mandelbrot`, `julia` and `newton` are pixel fractals. Image89 calculates the color of each pixel:

- `mandelbrot`: the pixel is the point `c` of z² + c, and z starts from 0. The color shows how fast
  z escapes to infinity (the smooth escape time). The points of the set get the first color of the palette.
- `julia`: the preset sets `c`, and the pixel is the start value of z.
- `newton`: Newton's method for zⁿ − 1 starts from the pixel. The color shows the root that it reaches.
  If the method does not converge, the pixel gets the background color.

The preset sets the visible area of the complex plane. The image always shows the whole area,
at any aspect ratio. One axis then shows more of the plane. Image89 does not stretch the image.

## Geometric figures

`sierpinski`, `koch` and `lissajous` are geometric figures. Image89 builds them as polygons, fits them into
the image with padding and without distortion, and draws them with anti-aliasing.

The level of detail depends on the image size:

- Sierpiński triangle: if the image is small, Image89 uses a lower depth. The smallest triangles must be
  at least 4 px wide. For example, a 32×32 image shows depth 2.
- Koch snowflake: if the image is small, Image89 uses a lower depth. Each segment must be at least 1 px long.
- Lissajous curve: with `points = 0`, Image89 uses ⌈π·R·max(a, b)/gcd(a, b)⌉ points, from 256 to 65 536.
  R is half of the shorter image side. From one point to the next, each coordinate changes by at most about 2 px.
  This is not true when the limit of 65 536 points applies.

The line width is `line_width` multiplied by the shorter image side, but at least 1 px. Lines have round joins.
The padding is also a fraction of the shorter image side.
