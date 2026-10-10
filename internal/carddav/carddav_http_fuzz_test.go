package carddav

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func FuzzCardDAVHandler(f *testing.F) {
	vc := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:u1\r\nFN:A\r\nEND:VCARD\r\n"
	f.Add("PUT", "/dav/addressbooks/default/u1.vcf", vc)
	f.Add("REPORT", "/dav/addressbooks/default/", `<c:addressbook-query xmlns:c="urn:ietf:params:xml:ns:carddav"><c:filter><c:prop-filter name="FN"><c:text-match>A</c:text-match></c:prop-filter></c:filter></c:addressbook-query>`)
	f.Add("REPORT", "/dav/addressbooks/default/", `<sync-collection xmlns="DAV:"><sync-token/></sync-collection>`)
	f.Add("PROPFIND", "/dav/addressbooks/", `<propfind xmlns="DAV:"><allprop/></propfind>`)
	f.Add("PROPPATCH", "/dav/addressbooks/default/", `<propertyupdate xmlns="DAV:"><set><prop><displayname>x</displayname></prop></set></propertyupdate>`)
	f.Add("MKCOL", "/dav/addressbooks/new/", `<mkcol xmlns="DAV:"/>`)
	f.Add("GET", "/dav/addressbooks/default/u1.vcf", "")
	f.Add("DELETE", "/dav/addressbooks/../x/%00", "")
	f.Fuzz(func(t *testing.T, method, path, body string) {
		if len(body) > 8192 || len(method) == 0 || strings.ContainsAny(method, " \r\n\t") ||
			!strings.HasPrefix(path, "/") || strings.ContainsAny(path, " \r\n\t#?") {
			t.Skip()
		}
		srv := NewServer(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		srv.SetAuthFunc(func(u, p string) (bool, error) { return true, nil })
		req := httptest.NewRequest("GET", "/", strings.NewReader(body))
		req.Method = method
		req.URL.Path = path
		req.SetBasicAuth("alice@example.com", "x")
		start := time.Now()
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code >= 500 && w.Code != 501 && w.Code != 507 {
			t.Logf("5xx %d for %s %s", w.Code, method, path)
		}
		if time.Since(start) > time.Second {
			t.Fatal("slow")
		}
	})
}
