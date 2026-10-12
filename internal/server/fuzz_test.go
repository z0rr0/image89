package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/encode"
	"github.com/z0rr0/image89/internal/figure"
	"github.com/z0rr0/image89/internal/pages"
	"github.com/z0rr0/image89/internal/render"
)

// discardLogs replaces the default logger for the rest of a fuzz target: the
// access log of every input would flood the output.
func discardLogs(f *testing.F) {
	f.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	f.Cleanup(func() { slog.SetDefault(prev) })
}

// validRequestID reports whether id may be echoed in a header and logged.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	return strings.Trim(id, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") == ""
}

// FuzzSanitizeRequestID sends a client X-Request-ID through LoggingMiddleware:
// the response echoes it only when it is a short token of safe characters,
// and otherwise a new ID of the same safe form.
func FuzzSanitizeRequestID(f *testing.F) {
	discardLogs(f)
	for _, s := range []string{
		"", "abc-123_XYZ", "123e4567-e89b-12d3-a456-426614174000", strings.Repeat("a", maxRequestIDLen),
		strings.Repeat("a", maxRequestIDLen+1), "a\r\nSet-Cookie: x=1", "id with space", "идентификатор", "a\x00b",
	} {
		f.Add(s)
	}
	h := LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	f.Fuzz(func(t *testing.T, id string) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		// Set keeps the value as is, so any bytes reach the middleware.
		req.Header.Set(requestIDHeader, id)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		got := rec.Header().Get(requestIDHeader)
		if !validRequestID(got) {
			t.Fatalf("X-Request-ID %q echoes %q, which is not a safe token", id, got)
		}
		if validRequestID(id) != (got == id) {
			t.Fatalf("X-Request-ID %q echoes %q: a valid ID must be kept and only a valid one", id, got)
		}
	})
}

// FuzzImageRoute sends image requests of any kind, size and format through
// the routes and handlers of the server with a stub renderer. A request
// reaches the renderer, and gets 200, exactly when the three parsers accept
// its parts; otherwise it gets a client error or a redirect of a path that is
// not clean, never a server error.
func FuzzImageRoute(f *testing.F) {
	discardLogs(f)
	for _, seed := range [][3]string{
		{"mandelbrot", "640x480", "png"}, {"julia", "32x32", "JPG"}, {"koch", "32769x32", ""},
		{"nope", "64x48", "gif"}, {"", "", ""}, {"..", "x", ""}, {"mandelbrot", "0640x480", "png"},
		{"lissajous", "64x48", "png&format=jpeg"}, {"newton", "64x48 ", "%00"}, {"sierpinski", "info", ""},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	p, err := pages.New("v-test", figure.Kinds())
	if err != nil {
		f.Fatalf("pages.New: %v", err)
	}
	stub := &stubRenderer{data: []byte("image")}
	s := New(config.Default(), stub, p, testVersion)

	f.Fuzz(func(t *testing.T, kind, size, format string) {
		stub.mu.Lock()
		stub.req = render.Request{}
		stub.mu.Unlock()

		target := "/" + url.PathEscape(kind) + "/" + url.PathEscape(size) + "/?format=" + url.QueryEscape(format)
		rec := httptest.NewRecorder()
		s.srv.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))

		wantKind, kindErr := figure.ParseKind(kind)
		wantSize, sizeErr := figure.ParseSize(size)
		wantFormat, formatErr := encode.Parse(format)
		valid := kindErr == nil && sizeErr == nil && formatErr == nil
		got := stub.last()
		switch rec.Code {
		case http.StatusOK:
			if size == "info" && kindErr == nil {
				return // the info page of the kind
			}
			want := render.Request{Kind: wantKind, Size: wantSize, Format: wantFormat}
			if !valid || got != want {
				t.Fatalf("GET %s rendered %+v, want %+v (kind %v, size %v, format %v)", target, got, want, kindErr, sizeErr, formatErr)
			}
		case http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusBadRequest, http.StatusNotFound:
			if got != (render.Request{}) {
				t.Fatalf("GET %s = %d, but the renderer got %+v", target, rec.Code, got)
			}
			if valid && rec.Code != http.StatusMovedPermanently {
				t.Fatalf("GET %s = %d for a valid request", target, rec.Code)
			}
		default:
			t.Fatalf("GET %s = %d: %s", target, rec.Code, rec.Body.String())
		}
	})
}
