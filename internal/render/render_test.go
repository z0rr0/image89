package render

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/encode"
	"github.com/z0rr0/image89/internal/figure"
)

var errBoom = errors.New("boom")

// genFunc is a Generator made of a function.
type genFunc func(ctx context.Context, size figure.Size) (*image.RGBA, error)

func (f genFunc) Generate(ctx context.Context, size figure.Size) (*image.RGBA, error) {
	return f(ctx, size)
}

// solid returns an opaque black image at once.
func solid(_ context.Context, size figure.Size) (*image.RGBA, error) {
	img := image.NewRGBA(image.Rect(0, 0, size.Width, size.Height))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xff
	}
	return img, nil
}

// testConfig returns the default config with one slot and the given limits.
func testConfig(maxQueued int, queueTimeout, timeout time.Duration) *config.Config {
	cfg := config.Default()
	cfg.Limits.MaxConcurrent = 1
	cfg.Limits.MaxQueued = maxQueued
	cfg.Limits.QueueTimeout = config.Duration(queueTimeout)
	cfg.Limits.Timeout = config.Duration(timeout)
	return cfg
}

// fakeRenderer returns a renderer of fake mandelbrot and julia generators.
func fakeRenderer(cfg *config.Config, mandelbrot, julia genFunc) *Renderer {
	return newRenderer(cfg, map[figure.Kind]Generator{figure.Mandelbrot: mandelbrot, figure.Julia: julia})
}

// request returns a valid 64×48 PNG request of kind.
func request(kind figure.Kind) Request {
	return Request{Kind: kind, Size: figure.Size{Width: 64, Height: 48}, Format: encode.PNG}
}

func discard([]byte) error { return nil }

// hold starts a mandelbrot Render whose sink blocks until the returned
// function is called and returns once the sink is entered, so the slot is
// taken. The returned function unblocks the sink and returns the Render error.
func hold(t *testing.T, r *Renderer) func() error {
	t.Helper()
	entered, unblock := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- r.Render(t.Context(), request(figure.Mandelbrot), func([]byte) error {
			close(entered)
			<-unblock
			return nil
		})
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("holding Render returned early: %v", err)
	}

	var once sync.Once
	release := func() { once.Do(func() { close(unblock) }) }
	t.Cleanup(release)
	return func() error {
		release()
		return <-done
	}
}

// renderAsync starts r.Render in a goroutine and returns the channel of its error.
func renderAsync(ctx context.Context, r *Renderer, req Request) <-chan error {
	done := make(chan error, 1)
	go func() { done <- r.Render(ctx, req, discard) }()
	return done
}

func TestValidate(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Limits.MaxPixels = 1 << 21
	r := fakeRenderer(cfg, solid, solid)
	size := func(w, h int) figure.Size { return figure.Size{Width: w, Height: h} }

	tests := []struct {
		name    string
		req     Request
		want    error
		notWant error
	}{
		{name: "png", req: Request{Kind: figure.Mandelbrot, Size: size(64, 48), Format: encode.PNG}},
		{name: "jpeg", req: Request{Kind: figure.Julia, Size: size(64, 48), Format: encode.JPEG}},
		{name: "png max side", req: Request{Kind: figure.Mandelbrot, Size: size(65536, 32), Format: encode.PNG}},
		{name: "jpeg max side", req: Request{Kind: figure.Mandelbrot, Size: size(65535, 32), Format: encode.JPEG}},
		{
			name: "unknown format", req: Request{Kind: figure.Mandelbrot, Size: size(64, 48), Format: "gif"},
			want: encode.ErrUnknownFormat,
		},
		{
			name: "empty format", req: Request{Kind: figure.Mandelbrot, Size: size(64, 48)},
			want: encode.ErrUnknownFormat,
		},
		{
			name: "format before kind", req: Request{Kind: "nope", Size: size(1, 1), Format: "gif"},
			want: encode.ErrUnknownFormat, notWant: figure.ErrUnknownKind,
		},
		{
			name: "kind without generator", req: Request{Kind: figure.Newton, Size: size(64, 48), Format: encode.PNG},
			want: figure.ErrUnknownKind,
		},
		{
			name: "kind before size", req: Request{Kind: "nope", Size: size(1, 1), Format: encode.PNG},
			want: figure.ErrUnknownKind, notWant: figure.ErrInvalidSize,
		},
		{
			name: "side below min", req: Request{Kind: figure.Mandelbrot, Size: size(31, 100), Format: encode.PNG},
			want: figure.ErrInvalidSize,
		},
		{
			name: "side above max", req: Request{Kind: figure.Mandelbrot, Size: size(65537, 32), Format: encode.PNG},
			want: figure.ErrInvalidSize,
		},
		{
			name: "too many pixels", req: Request{Kind: figure.Mandelbrot, Size: size(2048, 1025), Format: encode.PNG},
			want: ErrTooManyPixels,
		},
		{
			name: "pixels before side", req: Request{Kind: figure.Mandelbrot, Size: size(65536, 64), Format: encode.JPEG},
			want: ErrTooManyPixels, notWant: ErrSideTooLarge,
		},
		{
			name: "jpeg side", req: Request{Kind: figure.Mandelbrot, Size: size(65536, 32), Format: encode.JPEG},
			want: ErrSideTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := r.Validate(tt.req)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Validate(%+v) unexpected error: %v", tt.req, err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Errorf("Validate(%+v) error = %v, want %v", tt.req, err, tt.want)
			}
			if tt.notWant != nil && errors.Is(err, tt.notWant) {
				t.Errorf("Validate(%+v) error = %v, must not be %v", tt.req, err, tt.notWant)
			}
		})
	}
}

func TestRenderRejectsBeforeSlot(t *testing.T) {
	t.Parallel()

	invalid := request(figure.Mandelbrot)
	invalid.Size.Width = 10

	tests := []struct {
		name     string
		canceled bool
		req      Request
		want     error
	}{
		{name: "invalid request", req: invalid, want: figure.ErrInvalidSize},
		{name: "canceled ctx", canceled: true, req: request(figure.Mandelbrot), want: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			gen := func(ctx context.Context, size figure.Size) (*image.RGBA, error) {
				calls.Add(1)
				return solid(ctx, size)
			}
			r := fakeRenderer(testConfig(0, 0, time.Second), gen, gen)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.canceled {
				cancel()
			}

			err := r.Render(ctx, tt.req, discard)
			if !errors.Is(err, tt.want) {
				t.Errorf("Render error = %v, want %v", err, tt.want)
			}
			if n := calls.Load(); n != 0 {
				t.Errorf("generator called %d times, want 0", n)
			}
			if n := len(r.slots); n != 0 {
				t.Errorf("%d slots taken after Render, want 0", n)
			}
		})
	}
}

func TestRenderHoldsSlotUntilSinkReturns(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := fakeRenderer(testConfig(8, time.Second, time.Minute), solid, solid)
		release := hold(t, r)

		start := time.Now()
		err := r.Render(t.Context(), request(figure.Julia), discard)
		if !errors.Is(err, ErrBusy) || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Render error = %v, want %v and %v", err, ErrBusy, context.DeadlineExceeded)
		}
		if waited := time.Since(start); waited != time.Second {
			t.Errorf("waited %v for a slot, want queue_timeout %v", waited, time.Second)
		}

		if err = release(); err != nil {
			t.Fatalf("holding Render error: %v", err)
		}
		if err = r.Render(t.Context(), request(figure.Julia), discard); err != nil {
			t.Errorf("Render after the slot is released: %v", err)
		}
	})
}

func TestRenderWithoutWaiting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		maxQueued    int
		queueTimeout time.Duration
		want         error
	}{
		{name: "no queue", maxQueued: 0, queueTimeout: time.Minute, want: ErrQueueFull},
		{name: "no queue timeout", maxQueued: 8, queueTimeout: 0, want: context.DeadlineExceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				r := fakeRenderer(testConfig(tt.maxQueued, tt.queueTimeout, time.Minute), solid, solid)
				// The first request takes the free slot even with max_queued = 0.
				release := hold(t, r)

				start := time.Now()
				err := r.Render(t.Context(), request(figure.Julia), discard)
				if !errors.Is(err, tt.want) {
					t.Errorf("Render error = %v, want %v", err, tt.want)
				}
				if waited := time.Since(start); waited != 0 {
					t.Errorf("waited %v, want no waiting", waited)
				}
				if err = release(); err != nil {
					t.Errorf("holding Render error: %v", err)
				}
			})
		})
	}
}

func TestRenderQueue(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := fakeRenderer(testConfig(1, time.Minute, time.Minute), solid, solid)
		release := hold(t, r)

		second := renderAsync(t.Context(), r, request(figure.Julia))
		synctest.Wait() // the second request is blocked in the queue
		if n := r.waiting.Load(); n != 1 {
			t.Fatalf("%d requests wait, want 1", n)
		}

		err := r.Render(t.Context(), request(figure.Julia), discard)
		if !errors.Is(err, ErrQueueFull) {
			t.Errorf("third Render error = %v, want %v", err, ErrQueueFull)
		}

		if err = release(); err != nil {
			t.Errorf("holding Render error: %v", err)
		}
		if err = <-second; err != nil {
			t.Errorf("queued Render error: %v", err)
		}
		if n := r.waiting.Load(); n != 0 {
			t.Errorf("%d requests wait after the queue is drained, want 0", n)
		}
	})
}

func TestRenderCanceledWhileWaiting(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := fakeRenderer(testConfig(1, time.Minute, time.Minute), solid, solid)
		release := hold(t, r)

		ctx, cancel := context.WithCancel(t.Context())
		second := renderAsync(ctx, r, request(figure.Julia))
		synctest.Wait()
		cancel()

		err := <-second
		if !errors.Is(err, ErrBusy) || !errors.Is(err, context.Canceled) {
			t.Errorf("Render error = %v, want %v and %v", err, ErrBusy, context.Canceled)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Render error = %v, must not be %v", err, context.DeadlineExceeded)
		}
		if err = release(); err != nil {
			t.Errorf("holding Render error: %v", err)
		}
	})
}

// TestRenderTimeoutAfterSlot waits in the queue three times longer than the
// generation timeout: the generator still gets the whole timeout.
func TestRenderTimeoutAfterSlot(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const timeout = time.Second
		generated := make(chan time.Duration, 1)
		waitDone := func(ctx context.Context, _ figure.Size) (*image.RGBA, error) {
			start := time.Now()
			<-ctx.Done()
			generated <- time.Since(start)
			return nil, ctx.Err()
		}
		r := fakeRenderer(testConfig(1, time.Minute, timeout), solid, waitDone)
		release := hold(t, r)

		start := time.Now()
		second := renderAsync(t.Context(), r, request(figure.Julia))
		synctest.Wait()
		time.Sleep(3 * timeout)
		if err := release(); err != nil {
			t.Errorf("holding Render error: %v", err)
		}

		err := <-second
		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrBusy) {
			t.Errorf("Render error = %v, want %v of the generation", err, context.DeadlineExceeded)
		}
		if got := <-generated; got != timeout {
			t.Errorf("generation took %v, want timeout %v", got, timeout)
		}
		if total := time.Since(start); total != 4*timeout {
			t.Errorf("Render took %v, want %v of waiting plus %v of generation", total, 3*timeout, timeout)
		}
	})
}

func TestRenderErrors(t *testing.T) {
	t.Parallel()

	failing := func(context.Context, figure.Size) (*image.RGBA, error) { return nil, errBoom }
	tests := []struct {
		name string
		gen  genFunc
		sink func([]byte) error
		want error
	}{
		{name: "generator", gen: failing, sink: discard, want: errBoom},
		{name: "sink", gen: solid, sink: func([]byte) error { return errBoom }, want: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := fakeRenderer(testConfig(0, 0, time.Minute), tt.gen, solid)
			if err := r.Render(t.Context(), request(figure.Mandelbrot), tt.sink); !errors.Is(err, tt.want) {
				t.Errorf("Render error = %v, want %v", err, tt.want)
			}
			// With one slot and no queue, a slot left taken would fail this call.
			if err := r.Render(t.Context(), request(figure.Julia), discard); err != nil {
				t.Errorf("Render after an error: %v", err)
			}
		})
	}
}

func TestRenderEncodeCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The generator finishes, then the caller goes away before encoding.
	gen := func(ctx context.Context, size figure.Size) (*image.RGBA, error) {
		cancel()
		return solid(ctx, size)
	}
	r := fakeRenderer(testConfig(0, 0, time.Minute), gen, solid)

	called := false
	err := r.Render(ctx, request(figure.Mandelbrot), func([]byte) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Render error = %v, want %v", err, context.Canceled)
	}
	if called {
		t.Error("sink called after a failed encoding")
	}
}

func TestRenderPanicReleasesSlot(t *testing.T) {
	t.Parallel()

	panicking := func(context.Context, figure.Size) (*image.RGBA, error) { panic(errBoom) }
	r := fakeRenderer(testConfig(0, 0, time.Minute), panicking, solid)

	func() {
		defer func() {
			if p := recover(); p != errBoom { //nolint:errorlint // the panic value itself is compared
				t.Errorf("recovered %v, want the generator panic %v", p, errBoom)
			}
		}()
		_ = r.Render(t.Context(), request(figure.Mandelbrot), discard)
	}()

	if err := r.Render(t.Context(), request(figure.Julia), discard); err != nil {
		t.Errorf("Render after a panic: %v", err)
	}
}

func TestKinds(t *testing.T) {
	t.Parallel()

	r := New(config.Default())
	want := []figure.Kind{figure.Mandelbrot, figure.Julia}
	got := r.Kinds()
	if !slices.Equal(got, want) {
		t.Fatalf("Kinds() = %v, want %v", got, want)
	}
	got[0] = figure.Koch
	if again := r.Kinds(); !slices.Equal(again, want) {
		t.Errorf("Kinds() = %v after changing a returned slice, want %v", again, want)
	}

	only := newRenderer(config.Default(), map[figure.Kind]Generator{figure.Julia: genFunc(solid)})
	if got := only.Kinds(); !slices.Equal(got, []figure.Kind{figure.Julia}) {
		t.Errorf("Kinds() = %v, want [julia]", got)
	}
}

// TestNewRendersAllKinds renders every registered kind in both formats with
// the real generators and the default config.
func TestNewRendersAllKinds(t *testing.T) {
	t.Parallel()

	r := New(config.Default())
	decoders := map[encode.Format]func(io.Reader) (image.Image, error){
		encode.PNG:  png.Decode,
		encode.JPEG: jpeg.Decode,
	}
	size := figure.Size{Width: 64, Height: 48}

	for _, kind := range r.Kinds() {
		for format, decode := range decoders {
			t.Run(string(kind)+"_"+string(format), func(t *testing.T) {
				t.Parallel()

				var data []byte
				req := Request{Kind: kind, Size: size, Format: format}
				err := r.Render(t.Context(), req, func(b []byte) error {
					data = bytes.Clone(b)
					return nil
				})
				if err != nil {
					t.Fatalf("Render error: %v", err)
				}
				img, err := decode(bytes.NewReader(data))
				if err != nil {
					t.Fatalf("decode %s: %v", format, err)
				}
				if want := image.Rect(0, 0, size.Width, size.Height); img.Bounds() != want {
					t.Errorf("bounds = %v, want %v", img.Bounds(), want)
				}
			})
		}
	}
}

func BenchmarkRender(b *testing.B) {
	r := New(config.Default())
	req := Request{Kind: figure.Mandelbrot, Size: figure.Size{Width: 640, Height: 480}, Format: encode.PNG}
	b.ReportAllocs()
	for b.Loop() {
		if err := r.Render(b.Context(), req, discard); err != nil {
			b.Fatal(err)
		}
	}
}
