package autoconfig

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func FuzzAutoconfigRequests(f *testing.F) {
	f.Add("GET", "autoconfig.example.com", "/x?emailaddress=a@b.com", "")
	f.Add("POST", "example.com", "/autodiscover/autodiscover.xml", `<Autodiscover><Request><EMailAddress>a@b.com</EMailAddress></Request></Autodiscover>`)
	f.Add("POST", "x", "/?email=a@b", `<a><b>`)
	f.Add("GET", "[::1]:80", "/?email=%00", "")
	f.Add("GET", "a@b.com", "/", "")
	f.Add("GET", strings.Repeat("a.", 200)+"com", "/?emailaddress="+strings.Repeat("x", 300)+"@y", "")
	f.Fuzz(func(t *testing.T, method, host, target, body string) {
		if method != "GET" && method != "POST" {
			method = "GET"
		}
		req, err := http.NewRequest(method, "http://h/", strings.NewReader(body))
		if err != nil {
			t.Skip()
		}
		u, err := req.URL.Parse(target)
		if err != nil {
			t.Skip()
		}
		req.URL = u
		req.Host = host
		h := NewHandler(nil)
		start := time.Now()
		for _, fn := range []func(http.ResponseWriter, *http.Request){h.HandleAutoconfig, h.HandleAutodiscover} {
			w := httptest.NewRecorder()
			fn(w, req)
			if w.Code == 200 && !strings.Contains(w.Body.String(), "<") {
				t.Fatalf("200 with non-XML body")
			}
			// Invariant: output XML must not embed raw unescaped input markers.
			if w.Code == 200 && strings.Contains(w.Body.String(), "\x00") {
				t.Fatalf("NUL in output")
			}
		}
		if time.Since(start) > time.Second {
			t.Fatal("slow")
		}
	})
}
