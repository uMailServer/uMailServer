package auth

import (
	"context"
	"testing"
)

// F7241: RFC 7489 §7.1 external destination verification.
func TestR247_ExternalReportDestinationVerification(t *testing.T) {
	res := newMockDNSResolver()
	ctx := context.Background()
	// Same organizational domain: no verification needed.
	if ok, err := VerifyReportDestination(ctx, res, "example.com", "rua@mail.example.com"); !ok || err != nil {
		t.Fatalf("same org should be authorized: %v %v", ok, err)
	}
	// External without authorization record.
	if ok, _ := VerifyReportDestination(ctx, res, "example.com", "rua@evil.net"); ok {
		t.Fatal("external destination without record must be refused")
	}
	res.txtRecords["example.com._report._dmarc.evil.net"] = []string{"v=DMARC1"}
	if ok, err := VerifyReportDestination(ctx, res, "example.com", "rua@evil.net"); !ok || err != nil {
		t.Fatalf("authorized external should pass: %v %v", ok, err)
	}
	res.txtRecords["example.com._report._dmarc.other.net"] = []string{"v=spf1 -all"}
	if ok, _ := VerifyReportDestination(ctx, res, "example.com", "rua@other.net"); ok {
		t.Fatal("non-DMARC1 record must not authorize")
	}
}

// F7242: mailto scheme is case-insensitive; !size limit suffix is allowed.
func TestR247_ParseRUAEmail(t *testing.T) {
	for _, in := range []string{"MAILTO:a@example.com", "mailto:a@example.com!10m", "mailto:a@example.com!10m?subject=x"} {
		got, err := ParseRUAEmail(in)
		if err != nil || got != "a@example.com" {
			t.Errorf("%q => %q %v", in, got, err)
		}
	}
}

// F7243: dmarc-version = "v" *WSP "=" *WSP "DMARC1".
func TestR247_VersionTagWhitespace(t *testing.T) {
	if !isDMARCRecord("v = DMARC1; p=reject") {
		t.Fatal("whitespace around = must be accepted")
	}
	if isDMARCRecord("v=DMARC10; p=reject") || isDMARCRecord("p=none; v=DMARC1") {
		t.Fatal("must still reject")
	}
	rec, err := parseDMARCRecord("v = DMARC1; p=reject")
	if err != nil || rec.Policy != DMARCPolicyReject {
		t.Fatalf("%v %v", rec, err)
	}
}
