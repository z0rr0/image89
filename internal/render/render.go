// Package render is the image pipeline shared by the CLI and the server: it
// validates a request, waits for a generation slot, generates and encodes the
// image and hands the bytes over while the slot is still held.
package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"slices"
	"sync/atomic"
	"time"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/encode"
	"github.com/z0rr0/image89/internal/figure"
	"github.com/z0rr0/image89/internal/figure/fractal"
)

// Sentinel errors of Validate and Render.
var (
	// ErrTooManyPixels means width × height exceeds limits.max_pixels.
	ErrTooManyPixels = errors.New("too many pixels")
	// ErrSideTooLarge means an image side exceeds the largest side of the format.
	ErrSideTooLarge = errors.New("image side is too large for the format")
	// ErrQueueFull means limits.max_queued requests already wait for a slot.
	ErrQueueFull = errors.New("render queue is full")
	// ErrBusy means no slot became free in time; it is joined with the error
	// of the wait: context.DeadlineExceeded after limits.queue_timeout or
	// context.Canceled when the caller gave up.
	ErrBusy = errors.New("no free render slot")
)

// Generator draws an image of one figure kind. It must return an error soon
// after ctx is done and must be safe for concurrent use.
type Generator interface {
	Generate(ctx context.Context, size figure.Size) (*image.RGBA, error)
}

// Request is one image to render.
type Request struct {
	Kind   figure.Kind
	Size   figure.Size
	Format encode.Format
}

// Renderer renders images within the limits of the config. It is safe for
// concurrent use.
type Renderer struct {
	gens         map[figure.Kind]Generator
	kinds        []figure.Kind
	maxPixels    int64
	maxQueued    int64
	queueTimeout time.Duration
	timeout      time.Duration
	// slots holds a token for every image being generated or handed over.
	slots chan struct{}
	// waiting counts the requests waiting for a slot.
	waiting atomic.Int64
}

// New returns a renderer of every implemented figure kind. cfg must be valid
// and is not used after New returns.
func New(cfg *config.Config) *Renderer {
	workers := cfg.Limits.Workers
	return newRenderer(cfg, map[figure.Kind]Generator{
		figure.Mandelbrot: fractal.NewMandelbrot(cfg.Types.Mandelbrot, workers),
		figure.Julia:      fractal.NewJulia(cfg.Types.Julia, workers),
		figure.Newton:     fractal.NewNewton(cfg.Types.Newton, workers),
	})
}

// newRenderer returns a renderer of the given generators with the limits of
// cfg; tests pass fake generators here.
func newRenderer(cfg *config.Config, gens map[figure.Kind]Generator) *Renderer {
	kinds := slices.DeleteFunc(figure.Kinds(), func(k figure.Kind) bool {
		_, ok := gens[k]
		return !ok
	})
	l := &cfg.Limits
	return &Renderer{
		gens:         gens,
		kinds:        kinds,
		maxPixels:    l.MaxPixels,
		maxQueued:    int64(l.MaxQueued),
		queueTimeout: l.QueueTimeout.Timed(),
		timeout:      l.Timeout.Timed(),
		slots:        make(chan struct{}, l.MaxConcurrent),
	}
}

// Kinds returns the figure kinds this renderer can draw, in the order of
// figure.Kinds. Every call returns a new slice.
func (r *Renderer) Kinds() []figure.Kind {
	return slices.Clone(r.kinds)
}

// Validate checks req without allocating the image: the format is known
// (encode.ErrUnknownFormat), the kind has a generator (figure.ErrUnknownKind),
// the sides are in range (figure.ErrInvalidSize), the image has at most
// limits.max_pixels pixels (ErrTooManyPixels) and the format can encode its
// sides (ErrSideTooLarge). The first failed check is returned.
func (r *Renderer) Validate(req Request) error {
	if req.Format.ContentType() == "" {
		return errors.Join(encode.ErrUnknownFormat, fmt.Errorf("%q", req.Format))
	}
	if _, ok := r.gens[req.Kind]; !ok {
		return errors.Join(figure.ErrUnknownKind, fmt.Errorf("%q has no generator", req.Kind))
	}
	if err := req.Size.Validate(); err != nil {
		return err
	}
	if pixels := req.Size.Pixels(); pixels > r.maxPixels {
		return errors.Join(ErrTooManyPixels, fmt.Errorf("%s has %d pixels, limit %d", req.Size, pixels, r.maxPixels))
	}
	if side, limit := max(req.Size.Width, req.Size.Height), req.Format.MaxSide(); side > limit {
		return errors.Join(ErrSideTooLarge, fmt.Errorf("%s: %s sides are limited to %d", req.Size, req.Format, limit))
	}
	return nil
}

// Render validates req, takes a generation slot, generates and encodes the
// image and calls sink with the encoded bytes while the slot is still held, so
// limits.max_concurrent bounds the memory of the images being handed over too.
// The bytes are valid only until sink returns.
//
// A free slot is taken at once. Otherwise the request waits for one at most
// limits.queue_timeout (0 means no waiting), unless limits.max_queued requests
// are already waiting (ErrQueueFull). A failed wait gives ErrBusy joined with
// context.DeadlineExceeded or, when ctx is done, with its error. Generation and
// encoding are limited by limits.timeout counted from taking the slot. The
// slot is released when Render returns, also on a panic of the generator.
func (r *Renderer) Render(ctx context.Context, req Request, sink func([]byte) error) error {
	if err := r.Validate(req); err != nil {
		return err
	}
	// A caller that is already gone must not take a slot.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.acquire(ctx); err != nil {
		return err
	}
	defer r.release()

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	img, err := r.gens[req.Kind].Generate(ctx, req.Size)
	if err != nil {
		return fmt.Errorf("generate %s %s: %w", req.Kind, req.Size, err)
	}
	var buf bytes.Buffer
	if err = encode.Encode(ctx, &buf, img, req.Format); err != nil {
		return fmt.Errorf("encode %s %s %s: %w", req.Kind, req.Size, req.Format, err)
	}
	return sink(buf.Bytes())
}

// acquire takes a slot, waiting in the queue if none is free.
func (r *Renderer) acquire(ctx context.Context) error {
	select {
	case r.slots <- struct{}{}:
		return nil
	default:
	}

	if r.waiting.Add(1) > r.maxQueued {
		r.waiting.Add(-1)
		return errors.Join(ErrQueueFull, fmt.Errorf("%d requests already wait for a slot", r.maxQueued))
	}
	defer r.waiting.Add(-1)

	wait, cancel := context.WithTimeout(ctx, r.queueTimeout)
	defer cancel()
	select {
	case r.slots <- struct{}{}:
		return nil
	case <-wait.Done():
		return errors.Join(ErrBusy, wait.Err())
	}
}

// release frees a slot taken by acquire.
func (r *Renderer) release() {
	<-r.slots
}
