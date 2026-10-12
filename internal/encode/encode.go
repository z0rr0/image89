// Package encode is the single place that writes images as PNG or JPEG and
// knows the metadata of these formats. Encoding stops when its context is done.
package encode

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
)

// jpegQuality is the quality of JPEG images, 1..100.
const jpegQuality = 90

// ErrUnknownFormat means the format name is not png, jpeg or jpg.
var ErrUnknownFormat = errors.New("unknown image format")

// Format is an output image format.
type Format string

// Supported formats.
const (
	PNG  Format = "png"
	JPEG Format = "jpeg"
)

// Parse returns the format named by s, ignoring case: "" and "png" give PNG,
// "jpeg" and "jpg" give JPEG. Surrounding spaces are not trimmed.
func Parse(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "", "png":
		return PNG, nil
	case "jpeg", "jpg":
		return JPEG, nil
	}
	return "", errors.Join(ErrUnknownFormat, fmt.Errorf("%q", s))
}

// ContentType returns the MIME type of the format, or "" for an unknown format.
func (f Format) ContentType() string {
	switch f {
	case PNG:
		return "image/png"
	case JPEG:
		return "image/jpeg"
	default:
		return ""
	}
}

// Ext returns the file name extension with the dot, or "" for an unknown format.
func (f Format) Ext() string {
	switch f {
	case PNG:
		return ".png"
	case JPEG:
		return ".jpg"
	default:
		return ""
	}
}

// Encode writes img to w in the format f. Nothing is written if ctx is already
// done or f is unknown. Every write to w is preceded by a ctx check, so a
// canceled ctx stops the output at the next write, and ctx.Err() takes priority
// over the encoder error. PNG writes about every 32 KiB of compressed data and
// stops encoding at the first failed write; JPEG writes every 4 KiB but keeps
// encoding the rest of the image without writing it.
func Encode(ctx context.Context, w io.Writer, img image.Image, f Format) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	cw := writerFunc(func(p []byte) (int, error) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return w.Write(p)
	})

	var err error
	switch f {
	case PNG:
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		err = enc.Encode(cw, img)
	case JPEG:
		err = jpeg.Encode(cw, img, &jpeg.Options{Quality: jpegQuality})
	default:
		return errors.Join(ErrUnknownFormat, fmt.Errorf("%q", f))
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

// writerFunc is an io.Writer made of a function.
type writerFunc func(p []byte) (int, error)

// Write calls the function.
func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}
