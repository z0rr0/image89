package server

import (
	_ "embed"
	"net/http"
)

//go:embed favicon.ico
var favicon []byte

// handleFavicon serves the embedded icon without taking a render slot.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/vnd.microsoft.icon")
	if s.cacheControl != "" {
		w.Header().Set("Cache-Control", s.cacheControl)
	}
	serveStatic(w, r, "favicon.ico", favicon)
}
