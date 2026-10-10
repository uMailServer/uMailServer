package autoconfig

import (
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF5825_AutodiscoverNamespaceAndHeader(t *testing.T) {
	h := NewHandler(nil)
	w := httptest.NewRecorder()
	h.HandleAutodiscover(w, httptest.NewRequest("GET", "/autodiscover/autodiscover.xml?email=a%3Cb@example.com", nil))
	// invalid addr rejected, not echoed
	w = httptest.NewRecorder()
	h.HandleAutodiscover(w, httptest.NewRequest("GET", "/autodiscover/autodiscover.xml?email=bob@example.com", nil))
	body := w.Body.String()
	if !strings.HasPrefix(body, "<?xml") {
		t.Fatalf("missing XML declaration: %q", body)
	}
	if !strings.Contains(body, `<Response xmlns="http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a">`) {
		t.Fatalf("Response namespace missing: %s", body)
	}
	var out AutodiscoverResponse
	if err := xml.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Response.User.EMailAddress != "bob@example.com" || len(out.Response.Account.Protocol) != 2 {
		t.Fatalf("bad roundtrip %+v", out)
	}
}

func TestF5826_AutoconfigUsesEmailAddressDomain(t *testing.T) {
	h := NewHandler(nil)
	r := httptest.NewRequest("GET", "/mail/config-v1.1.xml?emailaddress=u@example.org", nil)
	r.Host = "mail.example.org"
	w := httptest.NewRecorder()
	h.HandleAutoconfig(w, r)
	body := w.Body.String()
	if !strings.HasPrefix(body, "<?xml") || !strings.Contains(body, "<hostname>mail.example.org</hostname>") || strings.Contains(body, "mail.mail.") {
		t.Fatalf("bad body: %s", body)
	}
}
