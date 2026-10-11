package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net"
	"strings"
	"testing"
)

// F6921: l= larger than the actual body must not verify (RFC 6376 §8.2).
func TestDKIMVerify_BodyLengthBeyondBodyFails_F6921(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	res := newMockDNSResolver()
	res.txtRecords["sel._domainkey.example.com"] = []string{"v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(pub)}
	signer := NewDKIMSignerEd25519(res, priv, "example.com", "sel")
	headers := map[string][]string{
		"from": {"a@example.com"}, "to": {"b@example.com"}, "subject": {"s"},
		"date": {"Mon, 1 Jan 2024 00:00:00 +0000"}, "message-id": {"<1@example.com>"},
	}
	body := []byte("hello\r\n")
	hdr, err := signer.Sign(headers, body)
	if err != nil {
		t.Fatal(err)
	}
	v := NewDKIMVerifier(res)
	if r, _, err := v.Verify(headers, body, hdr); r != DKIMPass {
		t.Fatalf("baseline: %v %v", r, err)
	}
	// Re-sign a header carrying l=<larger than body>.
	withL := strings.Replace(hdr, "bh=", "l=999; bh=", 1)
	sig, _ := parseDKIMSignature(withL)
	data := canonicalizeHeaders(headers, sig.SignedHeaders, sig.HeaderCanon) + dkimSignatureFieldForHash(withL, sig.HeaderCanon)
	b, _ := signEd25519(priv, []byte(data))
	withL = withL[:strings.LastIndex(withL, "b=")] + "b=" + b
	if r, _, _ := v.Verify(headers, body, withL); r == DKIMPass {
		t.Fatal("l= beyond body accepted")
	}
	// l= equal to body length still passes.
	exact := strings.Replace(hdr, "bh=", "l=7; bh=", 1)
	sig, _ = parseDKIMSignature(exact)
	data = canonicalizeHeaders(headers, sig.SignedHeaders, sig.HeaderCanon) + dkimSignatureFieldForHash(exact, sig.HeaderCanon)
	b, _ = signEd25519(priv, []byte(data))
	exact = exact[:strings.LastIndex(exact, "b=")] + "b=" + b
	if r, _, err := v.Verify(headers, body, exact); r != DKIMPass {
		t.Fatalf("l=exact: %v %v", r, err)
	}
}

// F6922: backslash is not an escape in tag-value lists.
func TestParseTagValueList_NoBackslashEscape_F6922(t *testing.T) {
	m := parseTagValueList(`v=DMARC1; p=none\; pct=50`)
	if m["pct"] != "50" || m["p"] != `none\` {
		t.Fatalf("got %#v", m)
	}
}

type r215MXResolver struct{ mx []*net.MX }

func (r *r215MXResolver) LookupTXT(context.Context, string) ([]string, error) { return nil, nil }
func (r *r215MXResolver) LookupIP(context.Context, string) ([]net.IP, error) {
	return nil, &net.DNSError{IsNotFound: true}
}
func (r *r215MXResolver) LookupMX(context.Context, string) ([]*net.MX, error) { return r.mx, nil }

// F6920: void lookups for per-MX-host address queries count toward the limit.
func TestSPF_MXHostVoidLookupsCounted_F6920(t *testing.T) {
	r := &r215MXResolver{mx: []*net.MX{{Host: "a.x."}, {Host: "b.x."}, {Host: "c.x."}}}
	c := NewSPFChecker(r)
	_, _, err := c.evaluateMX(context.Background(), net.ParseIP("192.0.2.1"), "", "example.com", 0, 0)
	if err == nil {
		t.Fatal("3 void MX-host lookups must be permerror")
	}
	_, _, err = c.evaluateMX(context.Background(), net.ParseIP("192.0.2.1"), "", "example.com", 0, 0)
	_ = err
	r.mx = r.mx[:2]
	if _, _, err = c.evaluateMX(context.Background(), net.ParseIP("192.0.2.1"), "", "example.com", 0, 0); err != nil {
		t.Fatalf("2 voids allowed: %v", err)
	}
}
