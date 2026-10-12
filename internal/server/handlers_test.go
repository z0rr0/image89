package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
	"github.com/z0rr0/image89/internal/render"
)

// fakeWriter is a ResponseWriter that counts WriteHeader calls. If writing is
// set, Write closes it and waits for release; err makes Write fail.
type fakeWriter struct {
	header  http.Header
	status  int
	headers int
	body    bytes.Buffer
	writing chan struct{}
	release chan struct{}
	err     error
}

func newFakeWriter() *fakeWriter {
	return &fakeWriter{header: make(http.Header)}
}

func (w *fakeWriter) Header() http.Header { return w.header }

func (w *fakeWriter) WriteHeader(status int) {
	w.headers++
	w.status = status
}

func (w *fakeWriter) Write(p []byte) (int, error) {
	if w.writing != nil {
		close(w.writing)
		<-w.release
	}
	if w.err != nil {
		return 0, w.err
	}
	return w.body.Write(p)
}

// limitsConfig returns the default config with one render slot.
func limitsConfig(maxQueued int, queueTimeout time.Duration) *config.Config {
	cfg := config.Default()
	cfg.Limits.MaxConcurrent = 1
	cfg.Limits.MaxQueued = maxQueued
	cfg.Limits.QueueTimeout = config.Duration(queueTimeout)
	cfg.Limits.Workers = 1
	return cfg
}

// decode decodes an image of the format and returns its bounds.
func decode(t *testing.T, body []byte, contentType string) image.Rectangle {
	t.Helper()
	decoders := map[string]func(*bytes.Reader) (image.Image, error){
		"image/png":  func(r *bytes.Reader) (image.Image, error) { return png.Decode(r) },
		"image/jpeg": func(r *bytes.Reader) (image.Image, error) { return jpeg.Decode(r) },
	}
	decoder, ok := decoders[contentType]
	if !ok {
		t.Fatalf("unexpected Content-Type %q", contentType)
	}
	img, err := decoder(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decode %s: %v", contentType, err)
	}
	return img.Bounds()
}

// TestImageKinds renders every registered kind through the real renderer.
func TestImageKinds(t *testing.T) {
	r := render.New(config.Default())
	s := newTestServer(t, config.Default(), r)
	for _, kind := range r.Kinds() {
		for _, format := range []string{"png", "jpeg"} {
			t.Run(fmt.Sprintf("%s_%s", kind, format), func(t *testing.T) {
				rec := serveRequest(t, s, http.MethodGet, fmt.Sprintf("/%s/64x48/?format=%s", kind, format))
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
				}
				h := rec.Header()
				if want := "image/" + format; h.Get("Content-Type") != want {
					t.Errorf("Content-Type = %q, want %q", h.Get("Content-Type"), want)
				}
				if want := strconv.Itoa(rec.Body.Len()); h.Get("Content-Length") != want {
					t.Errorf("Content-Length = %q, want %s", h.Get("Content-Length"), want)
				}
				if got := h.Get("Cache-Control"); got != "public, max-age=3600" {
					t.Errorf("Cache-Control = %q, want public, max-age=3600", got)
				}
				if bounds := decode(t, rec.Body.Bytes(), h.Get("Content-Type")); bounds != image.Rect(0, 0, 64, 48) {
					t.Errorf("bounds = %v, want 64x48", bounds)
				}
			})
		}
	}
}

func TestImageRequests(t *testing.T) {
	s := newTestServer(t, config.Default(), render.New(config.Default()))
	tests := []struct {
		name        string
		method      string
		target      string
		status      int
		contentType string
		location    string
	}{
		{name: "default format", target: "/mandelbrot/64x48/", status: http.StatusOK, contentType: "image/png"},
		{name: "format case", target: "/julia/64x48/?format=JPG", status: http.StatusOK, contentType: "image/jpeg"},
		{name: "unknown format", target: "/mandelbrot/64x48/?format=gif", status: http.StatusBadRequest},
		{
			name: "no trailing slash", target: "/mandelbrot/64x48?format=jpeg",
			status: http.StatusTemporaryRedirect, location: "/mandelbrot/64x48/?format=jpeg",
		},
		{name: "kind without slash", target: "/mandelbrot", status: http.StatusTemporaryRedirect, location: "/mandelbrot/"},
		{name: "extra segment", target: "/mandelbrot/64x48/extra", status: http.StatusNotFound},
		{name: "small size", target: "/mandelbrot/10x10/", status: http.StatusBadRequest},
		{name: "invalid size", target: "/mandelbrot/abc/", status: http.StatusBadRequest},
		{name: "unknown kind", target: "/nope/64x48/", status: http.StatusNotFound},
		{name: "kind without generator", target: "/sierpinski/64x48/", status: http.StatusNotFound},
		{name: "too many pixels", target: "/mandelbrot/8192x8192/", status: http.StatusBadRequest},
		{name: "side too large for jpeg", target: "/mandelbrot/65536x32/?format=jpeg", status: http.StatusBadRequest},
		{name: "post", method: http.MethodPost, target: "/mandelbrot/64x48/", status: http.StatusMethodNotAllowed},
		{name: "head", method: http.MethodHead, target: "/mandelbrot/64x48/", status: http.StatusOK, contentType: "image/png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := cmpOr(tt.method, http.MethodGet)
			rec := serveRequest(t, s, method, tt.target)
			if rec.Code != tt.status {
				t.Fatalf("%s %s = %d, want %d: %s", method, tt.target, rec.Code, tt.status, rec.Body.String())
			}
			h := rec.Header()
			if tt.contentType != "" && h.Get("Content-Type") != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", h.Get("Content-Type"), tt.contentType)
			}
			if got := h.Get("Location"); got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
			switch {
			case tt.status == http.StatusBadRequest || tt.status == http.StatusNotFound:
				if h.Get("Cache-Control") != "" {
					t.Errorf("error response has Cache-Control %q", h.Get("Cache-Control"))
				}
			case method == http.MethodHead:
				if rec.Body.Len() != 0 || h.Get("Content-Length") == "" || h.Get("Content-Length") == "0" {
					t.Errorf("HEAD body = %d bytes, Content-Length = %q, want an empty body and the image length",
						rec.Body.Len(), h.Get("Content-Length"))
				}
			}
		})
	}
}

// cmpOr returns value, or fallback if value is empty.
func cmpOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func TestImageNoCacheControl(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Server.CacheMaxAge = 0
	s := newTestServer(t, cfg, &stubRenderer{data: []byte("image")})
	rec := serveRequest(t, s, http.MethodGet, "/mandelbrot/64x48/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Errorf("Cache-Control = %q, want none for cache_max_age = 0", got)
	}
}

func TestImageDefaultSize(t *testing.T) {
	t.Parallel()

	r := &stubRenderer{data: []byte("image")}
	s := newTestServer(t, config.Default(), r)
	rec := serveRequest(t, s, http.MethodGet, "/julia/?format=jpeg")
	if rec.Code != http.StatusOK || rec.Body.String() != "image" {
		t.Fatalf("response = %d %q, want 200 image", rec.Code, rec.Body.String())
	}
	want := render.Request{Kind: figure.Julia, Size: figure.Size{Width: 1024, Height: 1024}, Format: "jpeg"}
	if got := r.last(); got != want {
		t.Errorf("request = %+v, want %+v", got, want)
	}
}

func TestImageTimeout(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Limits.Timeout = config.Duration(time.Nanosecond)
	s := newTestServer(t, cfg, render.New(cfg))
	rec := serveRequest(t, s, http.MethodGet, "/mandelbrot/64x48/")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Errorf("Retry-After = %q, want none for a generation timeout", got)
	}
}

func TestImageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int // 0 means no response
		retry  bool
		body   string
	}{
		{name: "queue full", err: errors.Join(render.ErrQueueFull, errors.New("8 wait")), status: http.StatusServiceUnavailable, retry: true},
		{name: "busy", err: errors.Join(render.ErrBusy, context.DeadlineExceeded), status: http.StatusServiceUnavailable, retry: true},
		{name: "timeout", err: fmt.Errorf("generate: %w", context.DeadlineExceeded), status: http.StatusServiceUnavailable},
		{name: "canceled in the queue", err: errors.Join(render.ErrBusy, context.Canceled)},
		{name: "canceled", err: fmt.Errorf("generate: %w", context.Canceled)},
		{name: "too many pixels", err: errors.Join(render.ErrTooManyPixels, errors.New("limit")), status: http.StatusBadRequest, body: "too many pixels\nlimit\n"},
		{name: "internal", err: errors.New("secret detail"), status: http.StatusInternalServerError, body: "Internal Server Error\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newTestServer(t, config.Default(), &stubRenderer{err: tt.err})
			w := newFakeWriter()
			s.srv.Handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mandelbrot/64x48/", nil))

			if tt.status == 0 {
				if w.headers != 0 || w.body.Len() != 0 {
					t.Errorf("response = %d %q, want none", w.status, w.body.String())
				}
				return
			}
			if w.status != tt.status || w.headers != 1 {
				t.Errorf("status = %d after %d WriteHeader calls, want %d once", w.status, w.headers, tt.status)
			}
			if got := w.header.Get("Retry-After") == "1"; got != tt.retry {
				t.Errorf("Retry-After = %q, want it %v", w.header.Get("Retry-After"), tt.retry)
			}
			if tt.body != "" && w.body.String() != tt.body {
				t.Errorf("body = %q, want %q", w.body.String(), tt.body)
			}
			if w.header.Get("Cache-Control") != "" {
				t.Errorf("error response has Cache-Control %q", w.header.Get("Cache-Control"))
			}
		})
	}
}

// TestImageWriteError checks that a failed write after the status is only logged.
func TestImageWriteError(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, config.Default(), &stubRenderer{data: []byte("image")})
	w := newFakeWriter()
	w.err = errors.New("broken pipe")
	s.srv.Handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mandelbrot/64x48/", nil))

	if w.status != http.StatusOK || w.headers != 1 {
		t.Errorf("status = %d after %d WriteHeader calls, want 200 once", w.status, w.headers)
	}
	if ct := w.header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
}

// TestImageBusy holds the only slot with a response blocked in Write. With
// max_queued = 0 the next request gets 503 at once. With max_queued = 1 the
// second request waits in the queue, the third gets 503 at once while the
// second waits, and the second gets 503 after queue_timeout. The bubble runs
// in virtual time without a network.
func TestImageBusy(t *testing.T) {
	t.Parallel()

	const queueTimeout = 2 * time.Second
	for _, maxQueued := range []int{0, 1} {
		t.Run(fmt.Sprintf("max_queued=%d", maxQueued), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				cfg := limitsConfig(maxQueued, queueTimeout)
				s := newTestServer(t, cfg, render.New(cfg))
				first, firstDone := holdSlot(t, s)
				start := time.Now()

				var queued *httptest.ResponseRecorder
				queuedDone := make(chan struct{})
				if maxQueued > 0 {
					go func() {
						defer close(queuedDone)
						queued = serveRequest(t, s, http.MethodGet, "/julia/64x48/")
					}()
					synctest.Wait() // the second request waits for the slot
				}

				rejected := serveRequest(t, s, http.MethodGet, "/newton/64x48/")
				checkBusy(t, "rejected", rejected, time.Since(start), 0)

				if maxQueued > 0 {
					<-queuedDone
					checkBusy(t, "queued", queued, time.Since(start), queueTimeout)
				}
				close(first.release)
				<-firstDone
				if first.status != http.StatusOK {
					t.Errorf("first response = %d, want 200", first.status)
				}
			})
		})
	}
}

// checkBusy checks a 503 response with Retry-After given after wait.
func checkBusy(t *testing.T, name string, rec *httptest.ResponseRecorder, elapsed, wait time.Duration) {
	t.Helper()
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Errorf("%s response = %d, Retry-After %q; want 503 and 1", name, rec.Code, rec.Header().Get("Retry-After"))
	}
	if elapsed != wait {
		t.Errorf("%s response after %v, want %v", name, elapsed, wait)
	}
}

// holdSlot starts a request whose response blocks in Write, so it holds a
// render slot until the returned writer is released; done is closed when the
// request ends.
func holdSlot(t *testing.T, s *Server) (*fakeWriter, <-chan struct{}) {
	t.Helper()
	w := newFakeWriter()
	w.writing, w.release = make(chan struct{}), make(chan struct{})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mandelbrot/64x48/", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.srv.Handler.ServeHTTP(w, req)
	}()
	<-w.writing
	return w, done
}

// TestImageCanceledInQueue checks a client that leaves while it waits for a
// slot: no response is written and the access log has status 0 at the info
// level. It is sequential, because it replaces the global logger.
func TestImageCanceledInQueue(t *testing.T) {
	var logs safeBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	synctest.Test(t, func(t *testing.T) {
		cfg := limitsConfig(1, time.Hour)
		s := newTestServer(t, cfg, render.New(cfg))
		first, firstDone := holdSlot(t, s)

		ctx, cancel := context.WithCancel(t.Context())
		second := newFakeWriter()
		secondDone := make(chan struct{})
		go func() {
			defer close(secondDone)
			s.srv.Handler.ServeHTTP(second, httptest.NewRequestWithContext(ctx, http.MethodGet, "/julia/64x48/", nil))
		}()
		synctest.Wait() // the second request waits for the slot
		cancel()
		<-secondDone

		if second.headers != 0 || second.body.Len() != 0 {
			t.Errorf("canceled response = %d %q, want none", second.status, second.body.String())
		}
		close(first.release)
		<-firstDone
	})

	if !strings.Contains(logs.String(), `level=INFO msg="request completed"`) ||
		!strings.Contains(logs.String(), "path=/julia/64x48/ status=0") {
		t.Errorf("logs have no info record with status 0 for the canceled request:\n%s", logs.String())
	}
}

func TestPages(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, config.Default(), &stubRenderer{})
	tests := []struct {
		name     string
		method   string
		target   string
		status   int
		contains string
		location string
	}{
		{name: "index", target: "/", status: http.StatusOK, contains: `<img loading="lazy" src="/newton/320x240/"`},
		{name: "index head", method: http.MethodHead, target: "/", status: http.StatusOK},
		{name: "info", target: "/mandelbrot/info/", status: http.StatusOK, contains: "<math"},
		{name: "info without slash", target: "/mandelbrot/info", status: http.StatusTemporaryRedirect, location: "/mandelbrot/info/"},
		{name: "info without page", target: "/julia/info/", status: http.StatusNotFound},
		{name: "info of unknown kind", target: "/nope/info/", status: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serveRequest(t, s, cmpOr(tt.method, http.MethodGet), tt.target)
			if rec.Code != tt.status {
				t.Fatalf("%s = %d, want %d", tt.target, rec.Code, tt.status)
			}
			if got := rec.Header().Get("Location"); got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
			if tt.status != http.StatusOK {
				return
			}
			h := rec.Header()
			if ct := h.Get("Content-Type"); ct != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
			}
			if h.Get("Content-Length") == "" {
				t.Error("no Content-Length")
			}
			if !strings.Contains(rec.Body.String(), tt.contains) {
				t.Errorf("body does not contain %q", tt.contains)
			}
		})
	}
}
