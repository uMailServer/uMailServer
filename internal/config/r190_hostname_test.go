package config

import "testing"

// F6670: server.hostname and domains[].dkim.selector flow into SMTP banners,
// HELO, Message-ID and DKIM s= tags; whitespace/CRLF/';' must be rejected.
func TestValidateHostnameAndSelectorR190(t *testing.T) {
	base := func() *Config {
		c := DefaultConfig()
		c.Server.Hostname = "mail.example.com"
		c.Server.DataDir = t.TempDir()
		return c
	}
	for _, h := range []string{"mail.example.com", "localhost", "mx-1.example.org.", "a_b.example.com"} {
		c := base()
		c.Server.Hostname = h
		if err := c.Validate(); err != nil {
			t.Errorf("hostname %q rejected: %v", h, err)
		}
	}
	for _, h := range []string{"mail example.com", "mail.example.com\r\n250 x", "a..b", "-.@x", "host/name", "mail;x.com"} {
		c := base()
		c.Server.Hostname = h
		if err := c.Validate(); err == nil {
			t.Errorf("hostname %q accepted", h)
		}
	}
	for _, sel := range []string{"default", "2024-01", "s1.k"} {
		c := base()
		c.Domains = []DomainConfig{{Name: "example.com", DKIM: DomainDKIMConfig{Selector: sel}}}
		if err := c.Validate(); err != nil {
			t.Errorf("selector %q rejected: %v", sel, err)
		}
	}
	for _, sel := range []string{"a;b=c", "sel ector", "a..b"} {
		c := base()
		c.Domains = []DomainConfig{{Name: "example.com", DKIM: DomainDKIMConfig{Selector: sel}}}
		if err := c.Validate(); err == nil {
			t.Errorf("selector %q accepted", sel)
		}
	}
}
