package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/umailserver/umailserver"
)

// handleAccountPortal serves the self-service account portal (web/account:
// password, two-factor, forwarding, vacation, filters) under /account/. It
// was embedded in the binary but served by no route, so users could not
// reach it. The portal shares the webmail's session cookie.
func (s *Server) handleAccountPortal(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/account" {
		http.Redirect(w, r, "/account/", http.StatusMovedPermanently)
		return
	}
	accountFS := s.accountFS
	if accountFS == nil {
		accountFS = newEmbedFSSub(umailserver.AccountFS, "web/account/dist")
	}

	// SPA routing: unknown paths serve index.html; missing assets are 404.
	path := strings.TrimPrefix(r.URL.Path, "/account/")
	if path == "" {
		path = "index.html"
	}
	file, err := openStaticFile(accountFS, path)
	if err != nil {
		if isMissingAsset(path) {
			http.NotFound(w, r)
			return
		}
		file, err = accountFS.Open("index.html")
		if err != nil {
			s.logger.Error("Failed to open account portal index.html", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		path = "index.html"
	}
	defer file.Close()

	switch {
	case strings.HasSuffix(path, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css")
	case strings.HasSuffix(path, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	}
	stat, err := file.Stat()
	if err != nil {
		s.logger.Error("Failed to stat file", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, path, stat.ModTime(), file.(io.ReadSeeker))
}
