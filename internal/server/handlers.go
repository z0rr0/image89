package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/z0rr0/image89/internal/encode"
	"github.com/z0rr0/image89/internal/figure"
	"github.com/z0rr0/image89/internal/pages"
	"github.com/z0rr0/image89/internal/render"
)

// statusNone means that no response is written: the client is gone or the
// server is stopping.
const statusNone = 0

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeText(w, http.StatusOK, "OK")
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeText(w, http.StatusOK, s.version)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.writePage(w, r, s.pages.Index())
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	kind, err := figure.ParseKind(r.PathValue("kind"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	page, err := s.pages.Info(kind)
	if err != nil {
		writeError(w, r, err)
		return
	}
	s.writePage(w, r, page)
}

// handleImage serves GET /{kind}/{size}/ and GET /{kind}/ (the default size)
// with the format from the query. The response is written by the sink under
// the render slot, so a slow client holds the slot until the image is sent.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	req, err := imageRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	sent := false
	err = s.renderer.Render(r.Context(), req, func(data []byte) error {
		h := w.Header()
		h.Set("Content-Type", req.Format.ContentType())
		h.Set("Content-Length", strconv.Itoa(len(data)))
		if s.cacheControl != "" {
			h.Set("Cache-Control", s.cacheControl)
		}
		w.WriteHeader(http.StatusOK)
		sent = true
		if r.Method == http.MethodHead {
			return nil
		}
		_, writeErr := w.Write(data) //nolint:gosec // G705: encoded image bytes with an image Content-Type
		return writeErr
	})
	switch {
	case err == nil:
	case sent:
		// The status is already sent, so only the log tells about the failure.
		slog.Info("image response interrupted", "request_id", requestIDFrom(r.Context()), "error", err)
	default:
		writeError(w, r, err)
	}
}

// imageRequest parses the kind, the size and the format of an image request.
// The route without a size gives the default size.
func imageRequest(r *http.Request) (render.Request, error) {
	kind, err := figure.ParseKind(r.PathValue("kind"))
	if err != nil {
		return render.Request{}, err
	}
	size := figure.Size{Width: figure.DefaultSide, Height: figure.DefaultSide}
	if value := r.PathValue("size"); value != "" {
		if size, err = figure.ParseSize(value); err != nil {
			return render.Request{}, err
		}
	}
	format, err := encode.Parse(r.URL.Query().Get("format"))
	if err != nil {
		return render.Request{}, err
	}
	return render.Request{Kind: kind, Size: size, Format: format}, nil
}

// errorStatus maps an error to the response status and tells whether the
// client should retry later. Errors may be joined, so context.Canceled is
// checked first: a request canceled while it waits for a slot carries
// render.ErrBusy too.
func errorStatus(err error) (int, bool) {
	switch {
	case errors.Is(err, context.Canceled):
		return statusNone, false
	case errors.Is(err, figure.ErrUnknownKind):
		return http.StatusNotFound, false
	case errors.Is(err, figure.ErrInvalidSize), errors.Is(err, render.ErrTooManyPixels),
		errors.Is(err, encode.ErrUnknownFormat):
		return http.StatusBadRequest, false
	case errors.Is(err, render.ErrQueueFull), errors.Is(err, render.ErrBusy):
		return http.StatusServiceUnavailable, true
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, false
	default:
		return http.StatusInternalServerError, false
	}
}

// writeError writes the response of a failed request. A client error gets the
// error text, a server error only its status text and a log record.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, retry := errorStatus(err)
	requestID := requestIDFrom(r.Context())
	switch status {
	case statusNone:
		return
	case http.StatusServiceUnavailable:
		slog.Warn("request rejected", "request_id", requestID, "error", err)
	case http.StatusInternalServerError:
		slog.Error("request failed", "request_id", requestID, "error", err)
	}

	if retry {
		w.Header().Set("Retry-After", "1")
	}
	msg := err.Error()
	if status >= http.StatusInternalServerError {
		msg = http.StatusText(status)
	}
	http.Error(w, msg, status)
}

func writeText(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, msg)
}

// writePage serves a page with its ETag and the cache header of images.
func (s *Server) writePage(w http.ResponseWriter, r *http.Request, page pages.Page) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("ETag", page.ETag)
	if s.cacheControl != "" {
		h.Set("Cache-Control", s.cacheControl)
	}
	serveStatic(w, r, "", page.Body)
}

// serveStatic serves content fixed for the process lifetime with
// http.ServeContent: it answers a matching If-None-Match with 304, HEAD
// without a body and Range with 206. An error response loses the cache
// headers set by the caller (see staticWriter).
func serveStatic(w http.ResponseWriter, r *http.Request, name string, content []byte) {
	http.ServeContent(staticWriter{w}, r, name, time.Time{}, bytes.NewReader(content))
}

// staticWriter removes Cache-Control and ETag from a response with an error
// status. http.ServeContent removes them for its own errors, such as 416, but
// writes 412 for a failed If-Match with the headers of the caller.
type staticWriter struct {
	http.ResponseWriter
}

func (w staticWriter) WriteHeader(status int) {
	if status >= http.StatusBadRequest {
		h := w.Header()
		h.Del("Cache-Control")
		h.Del("ETag")
	}
	w.ResponseWriter.WriteHeader(status)
}

// Unwrap allows http.ResponseController to reach optional interfaces provided
// by the underlying writer.
func (w staticWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
