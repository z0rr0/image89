package encode

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math/rand/v2"
	"testing"

	"github.com/z0rr0/image89/internal/figure"
)

var errBoom = errors.New("boom")

// countWriter counts the bytes written to it.
type countWriter struct {
	n int
}

func (w *countWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	return len(p), nil
}

// cancelWriter cancels the encoding context once limit bytes are written and
// counts the bytes that still arrive after that.
type cancelWriter struct {
	cancel      context.CancelFunc
	limit       int
	n           int
	afterCancel int
}

func (w *cancelWriter) Write(p []byte) (int, error) {
	if w.n >= w.limit {
		w.afterCancel += len(p)
	}
	w.n += len(p)
	if w.n >= w.limit {
		w.cancel()
	}
	return len(p), nil
}

// failWriter fails every write.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errBoom
}

// gradient returns an opaque 64×48 image with smooth color gradients.
func gradient() *image.RGBA {
	const w, h = 64, 48
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			i := img.PixOffset(x, y)
			img.Pix[i+0] = uint8(x * 255 / (w - 1) & 0xff)
			img.Pix[i+1] = uint8(y * 255 / (h - 1) & 0xff)
			img.Pix[i+2] = uint8((x + y) * 255 / (w + h - 2) & 0xff)
			img.Pix[i+3] = 0xff
		}
	}
	return img
}

// pattern returns an opaque w×h image that compresses moderately, unlike a
// plain gradient or noise.
func pattern(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			i := img.PixOffset(x, y)
			img.Pix[i+0] = uint8(x * y >> 8 & 0xff)
			img.Pix[i+1] = uint8((x ^ y) & 0xff)
			img.Pix[i+2] = uint8((x + y) >> 2 & 0xff)
			img.Pix[i+3] = 0xff
		}
	}
	return img
}

// noise returns an opaque w×h image of random pixels that does not compress.
func noise(t *testing.T, w, h int) *image.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if _, err := rand.NewChaCha8([32]byte{1}).Read(img.Pix); err != nil {
		t.Fatalf("fill noise: %v", err)
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xff
	}
	return img
}

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text    string
		want    Format
		wantErr bool
	}{
		{text: "", want: PNG},
		{text: "png", want: PNG},
		{text: "PNG", want: PNG},
		{text: "Png", want: PNG},
		{text: "jpeg", want: JPEG},
		{text: "jpg", want: JPEG},
		{text: "JPEG", want: JPEG},
		{text: "Jpg", want: JPEG},
		{text: "gif", wantErr: true},
		{text: "png ", wantErr: true},
		{text: " png", wantErr: true},
		{text: "jpe", wantErr: true},
		{text: "image/png", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()

			got, err := Parse(tt.text)
			if tt.wantErr {
				if !errors.Is(err, ErrUnknownFormat) {
					t.Fatalf("Parse(%q) error = %v, want %v", tt.text, err, ErrUnknownFormat)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.text, err)
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestFormatMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		format      Format
		contentType string
		ext         string
	}{
		{format: PNG, contentType: "image/png", ext: ".png"},
		{format: JPEG, contentType: "image/jpeg", ext: ".jpg"},
		{format: "gif"},
		{format: ""},
	}

	for _, tt := range tests {
		if got := tt.format.ContentType(); got != tt.contentType {
			t.Errorf("%q.ContentType() = %q, want %q", tt.format, got, tt.contentType)
		}
		if got := tt.format.Ext(); got != tt.ext {
			t.Errorf("%q.Ext() = %q, want %q", tt.format, got, tt.ext)
		}
	}
}

func TestEncodePNGRoundTrip(t *testing.T) {
	t.Parallel()

	src := gradient()
	var buf bytes.Buffer
	if err := Encode(t.Context(), &buf, src, PNG); err != nil {
		t.Fatalf("Encode(PNG) error: %v", err)
	}
	decoded, err := png.Decode(&buf)
	if err != nil {
		t.Fatalf("png.Decode error: %v", err)
	}
	got, ok := decoded.(*image.RGBA)
	if !ok {
		t.Fatalf("png.Decode returned %T, want *image.RGBA for an opaque image", decoded)
	}
	if got.Bounds() != src.Bounds() {
		t.Fatalf("decoded bounds = %v, want %v", got.Bounds(), src.Bounds())
	}
	if !bytes.Equal(got.Pix, src.Pix) {
		t.Error("decoded PNG pixels differ from the source")
	}
}

func TestEncodeJPEGRoundTrip(t *testing.T) {
	t.Parallel()

	src := gradient()
	var buf bytes.Buffer
	if err := Encode(t.Context(), &buf, src, JPEG); err != nil {
		t.Fatalf("Encode(JPEG) error: %v", err)
	}
	got, err := jpeg.Decode(&buf)
	if err != nil {
		t.Fatalf("jpeg.Decode error: %v", err)
	}
	b := src.Bounds()
	if got.Bounds() != b {
		t.Fatalf("decoded bounds = %v, want %v", got.Bounds(), b)
	}

	var sum [3]int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := got.At(x, y).RGBA()
			want := src.RGBAAt(x, y)
			sum[0] += absDiff(int(r>>8), int(want.R))
			sum[1] += absDiff(int(g>>8), int(want.G))
			sum[2] += absDiff(int(bl>>8), int(want.B))
		}
	}
	pixels := float64(b.Dx() * b.Dy())
	for i, name := range []string{"R", "G", "B"} {
		if mean := float64(sum[i]) / pixels; mean >= 3 {
			t.Errorf("mean JPEG error of %s = %.2f, want < 3", name, mean)
		}
	}
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// TestEncodeMaxSide checks that both formats encode the largest side of an
// image, so Validate needs no check per format. image/jpeg refuses a side of
// 1<<16 or more: figure.MaxSide must stay below it.
func TestEncodeMaxSide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		format        Format
		width, height int
		wantErr       bool
	}{
		{name: "png wide", format: PNG, width: figure.MaxSide, height: 1},
		{name: "png tall", format: PNG, width: 1, height: figure.MaxSide},
		{name: "jpeg wide", format: JPEG, width: figure.MaxSide, height: 1},
		{name: "jpeg tall", format: JPEG, width: 1, height: figure.MaxSide},
		{name: "jpeg limit", format: JPEG, width: 1 << 16, height: 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			img := image.NewRGBA(image.Rect(0, 0, tt.width, tt.height))
			var w countWriter
			err := Encode(t.Context(), &w, img, tt.format)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Encode(%q, %dx%d) error = nil, want an error", tt.format, tt.width, tt.height)
				}
				return
			}
			if err != nil {
				t.Fatalf("Encode(%q, %dx%d) unexpected error: %v", tt.format, tt.width, tt.height, err)
			}
			if w.n == 0 {
				t.Errorf("Encode(%q, %dx%d) wrote nothing", tt.format, tt.width, tt.height)
			}
		})
	}
}

func TestEncodeWritesNothing(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	tests := []struct {
		name   string
		ctx    context.Context //nolint:containedctx // test case input
		format Format
		want   error
	}{
		{name: "png canceled", ctx: canceled, format: PNG, want: context.Canceled},
		{name: "jpeg canceled", ctx: canceled, format: JPEG, want: context.Canceled},
		{name: "unknown format", ctx: t.Context(), format: "gif", want: ErrUnknownFormat},
		{name: "empty format", ctx: t.Context(), format: "", want: ErrUnknownFormat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var w countWriter
			err := Encode(tt.ctx, &w, gradient(), tt.format)
			if !errors.Is(err, tt.want) {
				t.Errorf("Encode error = %v, want %v", err, tt.want)
			}
			if w.n != 0 {
				t.Errorf("Encode wrote %d bytes, want 0", w.n)
			}
		})
	}
}

// TestEncodeCancelDuringWrite cancels ctx from inside a write once enough of
// incompressible output has arrived: no byte may be written after that. JPEG
// keeps encoding the rest of the image after a failed write, so its image is
// smaller to keep the test fast under -race.
func TestEncodeCancelDuringWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		format Format
		side   int
		limit  int
	}{
		{format: PNG, side: 4096, limit: 1 << 20},
		{format: JPEG, side: 1024, limit: 256 << 10},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			w := &cancelWriter{cancel: cancel, limit: tt.limit}
			err := Encode(ctx, w, noise(t, tt.side, tt.side), tt.format)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Encode error = %v, want %v", err, context.Canceled)
			}
			if w.n < tt.limit {
				t.Fatalf("Encode wrote %d bytes, the test needs at least %d", w.n, tt.limit)
			}
			if w.afterCancel != 0 {
				t.Errorf("Encode wrote %d bytes after cancel, want 0", w.afterCancel)
			}
		})
	}
}

func TestEncodeWriterError(t *testing.T) {
	t.Parallel()

	for _, format := range []Format{PNG, JPEG} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()

			if err := Encode(t.Context(), failWriter{}, gradient(), format); !errors.Is(err, errBoom) {
				t.Errorf("Encode error = %v, want %v", err, errBoom)
			}
		})
	}
}

func benchmarkEncode(b *testing.B, format Format) {
	b.Helper()
	img := pattern(1024, 1024)
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if err := Encode(ctx, io.Discard, img, format); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodePNG(b *testing.B) {
	benchmarkEncode(b, PNG)
}

func BenchmarkEncodeJPEG(b *testing.B) {
	benchmarkEncode(b, JPEG)
}
