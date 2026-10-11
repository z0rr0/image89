package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
)

// Color is an opaque RGB color written in TOML as "#rrggbb".
type Color struct {
	R, G, B uint8
}

// UnmarshalText implements encoding.TextUnmarshaler. It accepts exactly "#"
// followed by six hex digits in any case; short forms like "#fff" are rejected.
func (c *Color) UnmarshalText(text []byte) error {
	if len(text) != 7 || text[0] != '#' {
		return errors.Join(ErrInvalidColor, fmt.Errorf("%q, want #rrggbb", text))
	}
	var rgb [3]byte
	if _, err := hex.Decode(rgb[:], text[1:]); err != nil {
		return errors.Join(ErrInvalidColor, fmt.Errorf("%q: %w", text, err))
	}
	*c = Color{R: rgb[0], G: rgb[1], B: rgb[2]}
	return nil
}

// MarshalText implements encoding.TextMarshaler.
func (c Color) MarshalText() ([]byte, error) {
	return []byte(c.String()), nil
}

// String returns the color as "#rrggbb".
func (c Color) String() string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// RGBA returns the color as an opaque color.RGBA.
func (c Color) RGBA() color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: 0xff}
}
