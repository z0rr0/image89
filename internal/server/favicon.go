package server

import (
	"bytes"
	_ "embed"
	"net/http"
	"time"
)

//go:embed favicon.ico
var favicon []byte

// handleFavicon serves the embedded icon without taking a render slot.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/vnd.microsoft.icon")
	if s.cacheControl != "" {
		w.Header().Set("Cache-Control", s.cacheControl)
	}
	http.ServeContent(w, r, "favicon.ico", time.Time{}, bytes.NewReader(favicon))
}
