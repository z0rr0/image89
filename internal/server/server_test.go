package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
	"github.com/z0rr0/image89/internal/pages"
	"github.com/z0rr0/image89/internal/render"
)

const testVersion = "Image89: v-test"

// stubRenderer records the last request and answers with data through the
// sink or with err.
type stubRenderer struct {
	mu   sync.Mutex
	req  render.Request
	data []byte
	err  error
}

func (r *stubRenderer) Render(_ context.Context, req render.Request, sink func([]byte) error) error {
	r.mu.Lock()
	r.req = req
	r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	return sink(r.data)
}

func (r *stubRenderer) last() render.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.req
}

// blockingRenderer closes entered when Render starts, then waits for release
// or for ctx; canceled is closed if ctx ends the wait. On release it sends the
// error of ctx at that moment to released.
type blockingRenderer struct {
	entered  chan struct{}
	release  chan struct{}
	released chan error
	canceled chan struct{}
}

func newBlockingRenderer() *blockingRenderer {
	return &blockingRenderer{
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
		released: make(chan error, 1),
		canceled: make(chan struct{}),
	}
}

func (r *blockingRenderer) Render(ctx context.Context, _ render.Request, sink func([]byte) error) error {
	close(r.entered)
	select {
	case <-r.release:
		r.released <- ctx.Err()
		return sink([]byte("image"))
	case <-ctx.Done():
		close(r.canceled)
		return ctx.Err()
	}
}

// testPages returns the real pages of the three kinds that have generators.
func testPages(t *testing.T) *pages.Pages {
	t.Helper()
	p, err := pages.New("v-test", []figure.Kind{figure.Mandelbrot, figure.Julia, figure.Newton})
	if err != nil {
		t.Fatalf("pages.New: %v", err)
	}
	return p
}

// newTestServer returns a server with the real pages and r.
func newTestServer(t *testing.T, cfg *config.Config, r Renderer) *Server {
	t.Helper()
	return New(cfg, r, testPages(t), testVersion)
}

// serveRequest runs a request through the full handler of s without a network.
func serveRequest(t *testing.T, s *Server, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))
	return rec
}

// listen returns a listener on a free local port.
func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln
}

// startServer serves s on a free local port, closed when the test ends, and
// returns the base URL and the channel with the result of Serve.
func startServer(t *testing.T, s *Server) (string, <-chan error) {
	t.Helper()
	ln := listen(t)
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	t.Cleanup(func() { _ = s.srv.Close() })
	return "http://" + ln.Addr().String(), done
}

// get sends a GET request and returns the status and the body; it is called
// from other goroutines, so it reports errors instead of failing the test.
func get(ctx context.Context, url string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), err
}

func TestHealthAndVersion(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, config.Default(), &stubRenderer{})
	tests := []struct {
		target string
		want   string
	}{
		{target: "/health", want: "OK"},
		{target: "/version", want: testVersion},
	}
	for _, tt := range tests {
		rec := serveRequest(t, s, http.MethodGet, tt.target)
		if rec.Code != http.StatusOK || rec.Body.String() != tt.want {
			t.Errorf("%s = %d %q, want 200 %q", tt.target, rec.Code, rec.Body.String(), tt.want)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Errorf("%s Content-Type = %q, want text/plain", tt.target, ct)
		}
		if rec.Header().Get(requestIDHeader) == "" {
			t.Errorf("%s has no %s header", tt.target, requestIDHeader)
		}
	}
}

func TestNewConfig(t *testing.T) {
	t.Parallel()

	// Distinct values so a mixed-up field assignment cannot pass.
	cfg := config.Default()
	cfg.Server.Addr = "127.0.0.1:8089"
	cfg.Server.ReadTimeout = config.Duration(11 * time.Second)
	cfg.Server.ReadHeaderTimeout = config.Duration(3 * time.Second)
	cfg.Server.WriteTimeout = config.Duration(41 * time.Second)
	cfg.Server.IdleTimeout = config.Duration(61 * time.Second)
	s := New(cfg, &stubRenderer{}, testPages(t), testVersion)

	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"ReadTimeout", s.srv.ReadTimeout, 11 * time.Second},
		{"ReadHeaderTimeout", s.srv.ReadHeaderTimeout, 3 * time.Second},
		{"WriteTimeout", s.srv.WriteTimeout, 41 * time.Second},
		{"IdleTimeout", s.srv.IdleTimeout, 61 * time.Second},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
	if s.Addr() != cfg.Server.Addr {
		t.Errorf("Addr() = %q, want %q", s.Addr(), cfg.Server.Addr)
	}
	if s.srv.MaxHeaderBytes != maxHeaderBytes {
		t.Errorf("MaxHeaderBytes = %d, want %d", s.srv.MaxHeaderBytes, maxHeaderBytes)
	}
	if s.srv.ErrorLog == nil || s.srv.BaseContext == nil {
		t.Error("ErrorLog and BaseContext must be set")
	}
}

func TestCacheControl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		maxAge time.Duration
		want   string
	}{
		{maxAge: time.Hour, want: "public, max-age=3600"},
		{maxAge: 90*time.Second + 500*time.Millisecond, want: "public, max-age=90"},
		{maxAge: 500 * time.Millisecond, want: ""},
		{maxAge: 0, want: ""},
	}
	for _, tt := range tests {
		if got := cacheControl(config.Duration(tt.maxAge)); got != tt.want {
			t.Errorf("cacheControl(%v) = %q, want %q", tt.maxAge, got, tt.want)
		}
	}
}

// TestShutdownGraceful checks that Shutdown lets an active request finish:
// the request context stays alive during the drain and the client gets the image.
func TestShutdownGraceful(t *testing.T) {
	r := newBlockingRenderer()
	s := newTestServer(t, config.Default(), r)
	shutdownStarted := make(chan struct{})
	s.srv.RegisterOnShutdown(func() { close(shutdownStarted) })
	url, served := startServer(t, s)

	type result struct {
		status int
		body   string
		err    error
	}
	response := make(chan result, 1)
	go func() {
		status, body, err := get(t.Context(), url+"/mandelbrot/64x48/")
		response <- result{status: status, body: body, err: err}
	}()
	<-r.entered

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	shutdown := make(chan error, 1)
	go func() { shutdown <- s.Shutdown(ctx) }()
	<-shutdownStarted
	close(r.release)

	if err := <-shutdown; err != nil {
		t.Errorf("Shutdown error = %v, want nil", err)
	}
	// Shutdown started before the release, and the request context was still alive.
	if err := <-r.released; err != nil {
		t.Errorf("request context error during the drain = %v, want nil", err)
	}
	if res := <-response; res.err != nil || res.status != http.StatusOK || res.body != "image" {
		t.Errorf("response = %d %q %v, want 200 image", res.status, res.body, res.err)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("Serve error = %v, want %v", err, http.ErrServerClosed)
	}
}

// TestShutdownForced checks the fallback: when the deadline of Shutdown
// passes, the context of a hanging request is canceled and Shutdown returns.
func TestShutdownForced(t *testing.T) {
	r := newBlockingRenderer()
	s := newTestServer(t, config.Default(), r)
	url, served := startServer(t, s)

	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		_, _, _ = get(t.Context(), url+"/mandelbrot/64x48/")
	}()
	<-r.entered

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown error = %v, want %v", err, context.DeadlineExceeded)
	}
	select {
	case <-r.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("the request context was not canceled after the shutdown deadline")
	}
	<-requestDone
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("Serve error = %v, want %v", err, http.ErrServerClosed)
	}
}

// safeBuffer is a log buffer that handlers in other goroutines write to.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
