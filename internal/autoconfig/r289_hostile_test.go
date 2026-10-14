package autoconfig

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func r289wellFormed(t *testing.T, label string, body []byte) {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(body))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("%s: malformed XML: %v\n%s", label, err, body)
		}
	}
}

func TestR289Hostile(t *testing.T) {
	h := NewHandler(NewHostedProvider(nil, func(d string) bool { return d == "example.com" }))
	vals := []string{"a@example.com", "<x>&\"'@example.com", "\"a<b>&\"@example.com", "a\r\nX: y@example.com", "a@example.com\r\n", "ünï@example.com", "A@EXAMPLE.COM", "a@example.com.", strings.Repeat("a", 100000) + "@example.com", "a@evil.com", "a@\x00example.com", "Bob <a@example.com>", "a@b@example.com", "a@exa&mple.com", "a@<script>.com"}
	for _, v := range vals {
		for _, ep := range []string{"auto", "disc"} {
			q := url.Values{}
			var path string
			if ep == "auto" {
				q.Set("emailaddress", v)
				path = "/.well-known/autoconfig/mail/config-v1.1.xml?" + q.Encode()
			} else {
				q.Set("email", v)
				path = "/autodiscover/autodiscover.xml?" + q.Encode()
			}
			r := httptest.NewRequest("GET", path, nil)
			r.Host = "autoconfig.example.com"
			w := httptest.NewRecorder()
			if ep == "auto" {
				h.HandleAutoconfig(w, r)
			} else {
				h.HandleAutodiscover(w, r)
			}
			body := w.Body.Bytes()
			if w.Code == 200 {
				r289wellFormed(t, ep+":"+v[:min(len(v), 20)], body)
				if strings.Contains(string(body), "evil.com") {
					t.Fatalf("reflect %q", v)
				}
			}
			t.Logf("%s %.25q -> %d", ep, v, w.Code)
		}
	}
	// non-hosted via Host header
	for _, host := range []string{"autoconfig.evil.com", "evil.com", "autodiscover.EVIL.com:443"} {
		r := httptest.NewRequest("GET", "/.well-known/autoconfig/mail/config-v1.1.xml", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.HandleAutoconfig(w, r)
		if w.Code != 404 {
			t.Fatalf("%s -> %d", host, w.Code)
		}
	}
	// methods
	for _, m := range []string{"POST", "PUT", "DELETE", "HEAD", "OPTIONS"} {
		w := httptest.NewRecorder()
		h.HandleAutoconfig(w, httptest.NewRequest(m, "/x?emailaddress=a@example.com", nil))
		if w.Code != 405 {
			t.Fatalf("autoconfig %s -> %d", m, w.Code)
		}
	}
	for _, m := range []string{"PUT", "DELETE", "HEAD", "OPTIONS"} {
		w := httptest.NewRecorder()
		h.HandleAutodiscover(w, httptest.NewRequest(m, "/x?email=a@example.com", nil))
		if w.Code != 405 {
			t.Fatalf("autodiscover %s -> %d", m, w.Code)
		}
	}
}

func TestR289PostBomb(t *testing.T) {
	h := NewHandler(NewHostedProvider(nil, func(d string) bool { return d == "example.com" }))
	bomb := `<?xml version="1.0"?><!DOCTYPE lolz [<!ENTITY lol "lol">`
	prev := "lol"
	for i := 1; i < 10; i++ {
		bomb += `<!ENTITY lol` + string(rune('0'+i)) + ` "` + strings.Repeat("&"+prev+";", 10) + `">`
		prev = "lol" + string(rune('0'+i))
	}
	bomb += `]><Autodiscover><Request><EMailAddress>&` + prev + `;</EMailAddress></Request></Autodiscover>`
	cases := map[string]string{
		"bomb":  bomb,
		"deep":  strings.Repeat("<a>", 2000000),
		"big":   "<Autodiscover><Request><EMailAddress>" + strings.Repeat("a", 2<<20) + "</EMailAddress></Request></Autodiscover>",
		"valid": `<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/outlook/requestschema/2006"><Request><EMailAddress>a@example.com</EMailAddress></Request></Autodiscover>`,
	}
	for name, body := range cases {
		st := time.Now()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/autodiscover/autodiscover.xml", strings.NewReader(body))
		h.HandleAutodiscover(w, r)
		t.Logf("%s len=%d -> %d in %s", name, len(body), w.Code, time.Since(st))
		if time.Since(st) > 10*time.Second {
			t.Fatalf("unbounded work for %s", name)
		}
		if name == "big" && w.Code != 413 {
			t.Fatalf("big -> %d", w.Code)
		}
		if name == "valid" && w.Code != 200 {
			t.Fatalf("valid -> %d", w.Code)
		}
		if name == "bomb" && w.Code == 200 {
			t.Fatalf("bomb accepted")
		}
	}
}
