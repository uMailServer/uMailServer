package api

import (
	"net/http"
	"strings"
)

// F6703: many handlers answer 405 without the mandatory Allow header, and
// HEAD on a GET-only handler is 405 although HEAD must mirror GET. Both are
// fixed centrally: the router is wrapped so that HEAD is served by the GET
// handler (net/http drops the body) and any 405 lacking Allow gets the
// methods registered for the route.

// routeAllow returns the methods a route accepts, or "" when unknown.
func routeAllow(path string) string {
	switch path {
	case "/api/v1/auth/login", "/api/v1/auth/refresh", "/api/v1/account/password",
		"/api/v1/account/totp/setup", "/api/v1/account/totp/verify", "/api/v1/account/totp/disable",
		"/api/v1/push/subscribe", "/api/v1/push/test", "/api/v1/filters/reorder",
		"/api/v1/mail/send", "/api/v1/mail/move", "/api/v1/mail/flags",
		"/api/v1/admin/jwt/rotate", "/api/v1/cluster/failover", "/api/v1/cluster/heartbeat":
		return "POST"
	case "/api/v1/auth/logout":
		return "POST, DELETE"
	case "/api/v1/push/unsubscribe":
		return "DELETE"
	case "/api/v1/account/totp", "/api/v1/search", "/api/v1/threads", "/api/v1/threads/search",
		"/api/v1/push/vapid-public-key", "/api/v1/push/subscriptions", "/api/v1/admin/push/stats",
		"/api/v1/admin/vacations", "/api/v1/admin/jwt/status",
		"/api/v1/cluster/status", "/api/v1/cluster/instances", "/api/v1/mail/message",
		"/api/v1/admin/ratelimits/ip/", "/api/v1/admin/ratelimits/user/",
		"/.well-known/autoconfig/mail/config-v1.1.xml":
		return "GET, HEAD"
	case "/api/v1/mail/inbox", "/api/v1/mail/sent", "/api/v1/mail/drafts", "/api/v1/mail/trash", "/api/v1/mail/spam":
		return "GET, HEAD, POST"
	case "/api/v1/mail/delete":
		return "DELETE, POST"
	case "/api/v1/mail/draft":
		return "POST, PUT"
	case "/api/v1/vacation":
		return "GET, HEAD, PUT, DELETE"
	case "/api/v1/account/forwarding":
		return "GET, HEAD, PUT"
	case "/api/v1/filters":
		return "GET, HEAD, POST"
	case "/api/v1/admin/ratelimits/config":
		return "GET, HEAD, PUT"
	case "/autodiscover/autodiscover.xml":
		return "GET, HEAD, POST"
	}
	switch {
	case strings.HasPrefix(path, "/api/v1/filters/"):
		if strings.HasSuffix(path, "/toggle") {
			return "POST"
		}
		return "GET, HEAD, PUT, DELETE"
	case strings.HasPrefix(path, "/api/v1/threads/"):
		if strings.HasSuffix(path, "/read") {
			return "POST"
		}
		return "GET, HEAD, DELETE"
	case strings.HasPrefix(path, "/api/v1/admin/ratelimits/ip/"), strings.HasPrefix(path, "/api/v1/admin/ratelimits/user/"):
		return "GET, HEAD"
	}
	return ""
}

// allowWriter fills in Allow on a 405 that did not set one.
type allowWriter struct {
	http.ResponseWriter
	allow string
	wrote bool
}

func (a *allowWriter) WriteHeader(code int) {
	if !a.wrote {
		a.wrote = true
		if code == http.StatusMethodNotAllowed && a.allow != "" && a.Header().Get("Allow") == "" {
			a.Header().Set("Allow", a.allow)
		}
	}
	a.ResponseWriter.WriteHeader(code)
}

func (a *allowWriter) Write(b []byte) (int, error) {
	if !a.wrote {
		a.WriteHeader(http.StatusOK)
	}
	return a.ResponseWriter.Write(b)
}

func (a *allowWriter) Flush() {
	if f, ok := a.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (a *allowWriter) Unwrap() http.ResponseWriter { return a.ResponseWriter }

// methodMiddleware implements F6703 for the JSON API and the autoconfig route.
func (s *Server) methodMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allow := routeAllow(r.URL.Path)
		if allow == "" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodHead && strings.Contains(allow, "HEAD") {
			r2 := r.Clone(r.Context())
			r2.Method = http.MethodGet
			r = r2
		}
		next.ServeHTTP(&allowWriter{ResponseWriter: w, allow: allow}, r)
	})
}
