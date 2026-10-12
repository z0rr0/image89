// Package figure is the domain vocabulary of Image89: figure kinds, image
// sizes and their limits, and the row scheduler shared by the generators.
package figure

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Image side limits and the defaults used when a request gives no size or kind.
const (
	MinSide     = 32
	MaxSide     = 32768
	DefaultSide = 1024
	DefaultKind = Mandelbrot
)

// Sentinel errors of parsing and validation.
var (
	// ErrUnknownKind means the name is not one of Kinds.
	ErrUnknownKind = errors.New("unknown figure kind")
	// ErrInvalidSize means the size is not "<width>x<height>" or a side is out of [MinSide, MaxSide].
	ErrInvalidSize = errors.New("invalid image size")
)

// Kind is a figure type; its value is the name used in URLs, flags and config.
type Kind string

// Figure kinds in the order returned by Kinds.
const (
	Mandelbrot Kind = "mandelbrot"
	Julia      Kind = "julia"
	Newton     Kind = "newton"
	Sierpinski Kind = "sierpinski"
	Koch       Kind = "koch"
	Lissajous  Kind = "lissajous"
)

// Kinds returns all figure kinds in a stable order. Every call returns a new
// slice, so the caller may change it.
func Kinds() []Kind {
	return []Kind{Mandelbrot, Julia, Newton, Sierpinski, Koch, Lissajous}
}

// ParseKind returns the kind with the exact name s. Names are lowercase and
// case-sensitive, so every image has a single URL.
func ParseKind(s string) (Kind, error) {
	if k := Kind(s); slices.Contains(Kinds(), k) {
		return k, nil
	}
	return "", errors.Join(ErrUnknownKind, fmt.Errorf("%q", s))
}

// Size is an image size in pixels.
type Size struct {
	Width  int
	Height int
}

// ParseSize parses "<width>x<height>", e.g. "640x480". Sides are decimal
// numbers without a sign, spaces or leading zeros, and must pass Validate.
func ParseSize(s string) (Size, error) {
	w, h, ok := strings.Cut(s, "x")
	if !ok {
		return Size{}, errors.Join(ErrInvalidSize, fmt.Errorf("%q: want <width>x<height>", s))
	}
	width, okW := parseSide(w)
	height, okH := parseSide(h)
	if !okW || !okH {
		return Size{}, errors.Join(ErrInvalidSize, fmt.Errorf("%q: want <width>x<height>", s))
	}
	size := Size{Width: width, Height: height}
	if err := size.Validate(); err != nil {
		return Size{}, err
	}
	return size, nil
}

// Validate checks that both sides are in [MinSide, MaxSide].
func (s Size) Validate() error {
	if s.Width < MinSide || s.Width > MaxSide || s.Height < MinSide || s.Height > MaxSide {
		return errors.Join(ErrInvalidSize, fmt.Errorf("%dx%d: sides must be in [%d, %d]", s.Width, s.Height, MinSide, MaxSide))
	}
	return nil
}

// String returns the size as "<width>x<height>", the form ParseSize accepts.
func (s Size) String() string {
	return strconv.Itoa(s.Width) + "x" + strconv.Itoa(s.Height)
}

// Pixels returns Width × Height without overflow for any valid size.
func (s Size) Pixels() int64 {
	return int64(s.Width) * int64(s.Height)
}

// parseSide parses one side of a size. ParseUint already rejects signs,
// spaces, empty strings and values that overflow 32 bits.
func parseSide(s string) (int, bool) {
	if len(s) > 1 && s[0] == '0' {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return int(v), true
}
