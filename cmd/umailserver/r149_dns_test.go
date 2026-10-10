package main

import (
	"strings"
	"testing"
)

func TestDNSRecordLinesUsesStoredDKIM(t *testing.T) {
	out := dnsRecordLines("example.com", "sel1", "QUJD")
	for _, want := range []string{
		"example.com.    IN    MX    10    mail.example.com.",
		"v=spf1 mx ~all",
		"sel1._domainkey.example.com.    IN    TXT    \"v=DKIM1; k=rsa; p=QUJD\"",
		"v=DMARC1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<GENERATE") {
		t.Error("placeholder leaked")
	}
	if !strings.Contains(dnsRecordLines("example.com", "", ""), "no DKIM public key stored") {
		t.Error("expected honest notice when key missing")
	}
}
