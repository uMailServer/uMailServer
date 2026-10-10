// Regression tests for F5663 and F5664: principal discovery (RFC 6352 §7.1.1,
// RFC 5397) and the /.well-known/carddav bootstrap redirect (RFC 6764 §6).

package carddav

import (
	"encoding/xml"
	"net/http"
	"strings"
	"testing"
)

func TestCardDAVDiscoveryF5663Control(t *testing.T) {
	s := discSetup(t)
	w := discDo(s, "PROPFIND", "/dav/addressbooks/ab1/", "", map[string]string{"Depth": "0"})
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND book = %d", w.Code)
	}
}

func TestCardDAVDiscoveryF5663Failure(t *testing.T) {
	s := discSetup(t)
	body := `<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav"><D:prop><D:current-user-principal/><C:addressbook-home-set/><D:resourcetype/></D:prop></D:propfind>`
	// PROPFIND on the principal URL must describe the principal itself.
	w := discDo(s, "PROPFIND", "/dav/principals/alice@x.test/", body, map[string]string{"Depth": "0"})
	ms, ok := discParse(t, w)
	if !ok {
		return
	}
	if len(ms.Responses) != 1 || ms.Responses[0].Href != "/dav/principals/alice@x.test/" {
		var hrefs []string
		for _, r := range ms.Responses {
			hrefs = append(hrefs, r.Href)
		}
		t.Errorf("DEFECT F5663: principal PROPFIND\nEXPECTED: one response for /dav/principals/alice@x.test/\nACTUAL: %v", hrefs)
		return
	}
	p := discProps(ms, "/dav/principals/alice@x.test/")
	cup := p[xml.Name{Local: "current-user-principal"}]
	if len(cup.Children) != 1 || strings.TrimSpace(cup.Children[0].Text) != "/dav/principals/alice@x.test/" {
		t.Errorf("DEFECT F5663: current-user-principal = %+v", cup)
	}
}

func TestCardDAVDiscoveryF5663Edges(t *testing.T) {
	s := discSetup(t)
	// Another user's principal is not served.
	if w := discDo(s, "PROPFIND", "/dav/principals/bob@x.test/", "", map[string]string{"Depth": "0"}); w.Code != http.StatusNotFound {
		t.Errorf("DEFECT F5663: foreign principal = %d, want 404", w.Code)
	}
	// Discovery entry points answer current-user-principal for the root URLs,
	// under the request-URI's own href.
	body := `<D:propfind xmlns:D="DAV:"><D:prop><D:current-user-principal/></D:prop></D:propfind>`
	for _, path := range []string{"/", "/dav/"} {
		w := discDo(s, "PROPFIND", path, body, map[string]string{"Depth": "0"})
		ms, ok := discParse(t, w)
		if !ok {
			return
		}
		cup := discProps(ms, path)[xml.Name{Local: "current-user-principal"}]
		if len(cup.Children) != 1 || strings.TrimSpace(cup.Children[0].Text) != "/dav/principals/alice@x.test/" {
			t.Errorf("DEFECT F5663: %s current-user-principal = %+v", path, cup)
		}
	}
	// The home resource also names the principal.
	w := discDo(s, "PROPFIND", "/dav/addressbooks/", body, map[string]string{"Depth": "0"})
	ms, ok := discParse(t, w)
	if !ok {
		return
	}
	if cup := discProps(ms, "/dav/addressbooks/")[xml.Name{Local: "current-user-principal"}]; len(cup.Children) != 1 {
		t.Errorf("DEFECT F5663: home current-user-principal = %+v", cup)
	}
}

func TestCardDAVDiscoveryF5664Control(t *testing.T) {
	s := discSetup(t)
	if w := discDo(s, "PROPFIND", "/dav/", "", map[string]string{"Depth": "0"}); w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND /dav/ = %d", w.Code)
	}
}

func TestCardDAVDiscoveryF5664Failure(t *testing.T) {
	s := discSetup(t)
	for _, method := range []string{"GET", "PROPFIND"} {
		w := discDo(s, method, "/.well-known/carddav", "", nil)
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/dav/" {
			t.Errorf("DEFECT F5664: %s /.well-known/carddav\nEXPECTED: 301 Location /dav/\nACTUAL: %d Location %q", method, w.Code, w.Header().Get("Location"))
		}
	}
}

func TestCardDAVDiscoveryF5664Edges(t *testing.T) {
	s := discSetup(t)
	// The redirect does not need credentials (clients probe it first) and
	// exposes nothing but a constant path.
	req, _ := http.NewRequest("GET", "/.well-known/carddav", nil)
	w := &recorder{h: http.Header{}}
	s.ServeHTTP(w, req)
	if w.code != http.StatusMovedPermanently || w.h.Get("Location") != "/dav/" {
		t.Errorf("DEFECT F5664: unauthenticated well-known = %d %q", w.code, w.h.Get("Location"))
	}
	// Other well-known names are not hijacked; they still require auth.
	req2, _ := http.NewRequest("GET", "/.well-known/other", nil)
	w2 := &recorder{h: http.Header{}}
	s.ServeHTTP(w2, req2)
	if w2.code != http.StatusUnauthorized {
		t.Errorf("DEFECT F5664: /.well-known/other = %d, want 401", w2.code)
	}
}

type recorder struct {
	h    http.Header
	code int
}

func (r *recorder) Header() http.Header         { return r.h }
func (r *recorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *recorder) WriteHeader(c int)           { r.code = c }
