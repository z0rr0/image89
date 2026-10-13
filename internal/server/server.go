// Package server implements the Image89 HTTP server: images, static pages,
// health and version endpoints, wrapped with logging and panic-recovery
// middleware.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/figure"
	"github.com/z0rr0/image89/internal/render"
)

const maxHeaderBytes = 1 << 20 // 1 MiB

// Renderer renders images; *render.Renderer implements it. Render validates
// the request before it waits for a slot and calls sink under the slot.
type Renderer interface {
	Render(ctx context.Context, req render.Request, sink func([]byte) error) error
}

// Pages holds the static HTML pages; *pages.Pages implements it. The returned
// bytes are shared and must not be modified.
type Pages interface {
	Index() []byte
	// Info returns the page of the kind or an error with figure.ErrUnknownKind.
	Info(kind figure.Kind) ([]byte, error)
}

// Server wraps the standard library HTTP server with the Image89 routes.
type Server struct {
	srv      *http.Server
	renderer Renderer
	pages    Pages
	version  string
	// cacheControl is the Cache-Control header of images, empty for none.
	cacheControl string
	// cancelBase cancels the base context of every request.
	cancelBase context.CancelFunc
}

// New builds the HTTP server. Requests get a base context of their own, not
// derived from any caller context: a stop signal must not cancel the requests
// that Shutdown lets finish.
func New(cfg *config.Config, r Renderer, p Pages, version string) *Server {
	base, cancel := context.WithCancel(context.Background())
	s := &Server{
		renderer:     r,
		pages:        p,
		version:      version,
		cacheControl: cacheControl(cfg.Server.CacheMaxAge),
		cancelBase:   cancel,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /version", s.handleVersion)
	mux.HandleFunc("GET /favicon.ico", s.handleFavicon)
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /{kind}/{$}", s.handleImage)
	mux.HandleFunc("GET /{kind}/{size}/{$}", s.handleImage)
	mux.HandleFunc("GET /{kind}/info/{$}", s.handleInfo)

	// LoggingMiddleware recovers handler panics so it can record their final
	// status. RecoverMiddleware remains outermost as a last line of defense for
	// panics raised by logging itself.
	handler := RecoverMiddleware(LoggingMiddleware(mux))

	s.srv = &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           handler,
		ReadTimeout:       cfg.Server.ReadTimeout.Timed(),
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout.Timed(),
		WriteTimeout:      cfg.Server.WriteTimeout.Timed(),
		IdleTimeout:       cfg.Server.IdleTimeout.Timed(),
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return base },
	}
	return s
}

// cacheControl returns the Cache-Control header of images for maxAge, or an
// empty string when maxAge is shorter than a second.
func cacheControl(maxAge config.Duration) string {
	seconds := int64(maxAge.Timed() / time.Second)
	if seconds <= 0 {
		return ""
	}
	return "public, max-age=" + strconv.FormatInt(seconds, 10)
}

// Addr returns the configured address of the server.
func (s *Server) Addr() string { return s.srv.Addr }

// Serve accepts connections on ln until Shutdown; it always returns an error,
// http.ErrServerClosed after Shutdown.
func (s *Server) Serve(ln net.Listener) error {
	return s.srv.Serve(ln)
}

// Shutdown stops the server gracefully: it closes the listeners and waits for
// the active requests to finish. If ctx is done first, it cancels the context
// of the remaining requests, so their generation stops, closes their
// connections and returns the error of ctx. The base context of the requests
// is canceled when Shutdown returns.
func (s *Server) Shutdown(ctx context.Context) error {
	defer s.cancelBase()
	err := s.srv.Shutdown(ctx)
	if err != nil {
		s.cancelBase()
		if closeErr := s.srv.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}
	return err
}
