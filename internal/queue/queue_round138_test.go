package queue

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/db"
)

type pubResolver struct{ txt map[string]string }

func (r *pubResolver) LookupTXT(ctx context.Context, n string) ([]string, error) {
	if v, ok := r.txt[n]; ok {
		return []string{v}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: n, IsNotFound: true}
}
func (r *pubResolver) LookupIP(ctx context.Context, h string) ([]net.IP, error)  { return nil, nil }
func (r *pubResolver) LookupMX(ctx context.Context, d string) ([]*net.MX, error) { return nil, nil }

func r138Mgr(t *testing.T, doms ...*db.DomainData) *Manager {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, dd := range doms {
		if err := d.CreateDomain(dd); err != nil {
			t.Fatal(err)
		}
	}
	return NewManager(d, nil, t.TempDir(), nil)
}

func r138Verify(t *testing.T, signed []byte, res *pubResolver) auth.DKIMResult {
	t.Helper()
	// What the receiver sees: the SMTP DATA writer converts bare LF to CRLF.
	s := strings.ReplaceAll(strings.ReplaceAll(string(signed), "\r\n", "\n"), "\n", "\r\n")
	idx := strings.Index(s, "\r\n")
	for idx+2 < len(s) && (s[idx+2] == ' ' || s[idx+2] == '\t') {
		n := strings.Index(s[idx+2:], "\r\n")
		idx += 2 + n
	}
	dkimLine := strings.TrimPrefix(s[:idx], "DKIM-Signature:")
	rest := []byte(s[idx+2:])
	v := auth.NewDKIMVerifier(res)
	hdrs := parseMessageHeaders(rest)
	r, _, err := v.Verify(hdrs, extractMessageBody(rest), dkimLine)
	if err != nil {
		t.Logf("verify err: %v", err)
	}
	return r
}

func rsaDomain(t *testing.T, name string) (*db.DomainData, string) {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
	pub, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return &db.DomainData{Name: name, DKIMSelector: "s1", DKIMPrivateKey: pemKey},
		"v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(pub)
}

// F6200: LF-only message must verify after the wire CRLF conversion.
func TestR138_F6200_BareLFRoundTrip(t *testing.T) {
	dd, txt := rsaDomain(t, "example.com")
	m := r138Mgr(t, dd)
	msg := []byte("From: a@example.com\nTo: b@remote.test\nSubject: hi\nDate: Mon, 1 Jan 2024 00:00:00 +0000\nMessage-ID: <1@example.com>\n\nline1\nline2\n")
	signed, err := m.signWithDKIM("a@example.com", msg)
	if err != nil {
		t.Fatal(err)
	}
	if r := r138Verify(t, signed, &pubResolver{map[string]string{"s1._domainkey.example.com": txt}}); r != auth.DKIMPass {
		t.Fatalf("want pass, got %v", r)
	}
}

// F6201: sign with From-header domain, not a foreign/bounce envelope domain.
func TestR138_F6201_FromDomainPreferred(t *testing.T) {
	dd, txt := rsaDomain(t, "example.com")
	other, _ := rsaDomain(t, "relay.test")
	m := r138Mgr(t, dd, other)
	msg := []byte("From: a@example.com\r\nTo: b@remote.test\r\nSubject: hi\r\nDate: Mon, 1 Jan 2024 00:00:00 +0000\r\nMessage-ID: <1@example.com>\r\n\r\nbody\r\n")
	signed, err := m.signWithDKIM("bounce@relay.test", msg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(signed[:200]), "d=example.com") {
		t.Fatalf("expected d=example.com, got %s", signed[:200])
	}
	if r := r138Verify(t, signed, &pubResolver{map[string]string{"s1._domainkey.example.com": txt}}); r != auth.DKIMPass {
		t.Fatalf("want pass, got %v", r)
	}
}

// F6201b: never sign for a domain we do not own.
func TestR138_F6201_NeverSignForeign(t *testing.T) {
	dd, _ := rsaDomain(t, "example.com")
	m := r138Mgr(t, dd)
	msg := []byte("From: a@foreign.test\r\nTo: b@remote.test\r\n\r\nbody\r\n")
	if _, err := m.signWithDKIM("a@foreign.test", msg); err == nil {
		t.Fatal("must not sign for foreign domain")
	}
}

// F6202: Ed25519 and PKCS8 keys.
func TestR138_F6202_Ed25519AndPKCS8(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(priv)
	dd := &db.DomainData{Name: "ed.test", DKIMSelector: "e1", DKIMPrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))}
	m := r138Mgr(t, dd)
	msg := []byte("From: a@ed.test\r\nTo: b@remote.test\r\nSubject: x\r\nDate: Mon, 1 Jan 2024 00:00:00 +0000\r\nMessage-ID: <1@ed.test>\r\n\r\nbody\r\n")
	signed, err := m.signWithDKIM("a@ed.test", msg)
	if err != nil {
		t.Fatal(err)
	}
	txt := "v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(pub)
	if r := r138Verify(t, signed, &pubResolver{map[string]string{"e1._domainkey.ed.test": txt}}); r != auth.DKIMPass {
		t.Fatalf("want pass, got %v", r)
	}
}

// F6203: Bcc and Return-Path never go to remote servers; CRLF normalised.
func TestR138_F6203_StripBccReturnPath(t *testing.T) {
	msg := []byte("Return-Path: <x@y>\nFrom: a@b\nBcc: secret@c,\n  folded@d\nTo: t@e\nSubject: s\n\nBcc: in body stays\n")
	out := string(prepareOutbound(msg))
	if strings.Contains(out, "secret") || strings.Contains(out, "folded") || strings.Contains(out, "Return-Path") {
		t.Fatalf("leak: %q", out)
	}
	if !strings.Contains(out, "Bcc: in body stays\r\n") || !strings.Contains(out, "To: t@e\r\n") || strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Fatalf("bad output: %q", out)
	}
}

// F6204: key never appears in errors.
func TestR138_F6204_BadKeyNotLogged(t *testing.T) {
	dd := &db.DomainData{Name: "bad.test", DKIMSelector: "s", DKIMPrivateKey: "-----BEGIN PRIVATE KEY-----\nQUJDREVGR0g=\n-----END PRIVATE KEY-----\n"}
	m := r138Mgr(t, dd)
	_, err := m.signWithDKIM("a@bad.test", []byte("From: a@bad.test\r\n\r\nx\r\n"))
	if err == nil || strings.Contains(err.Error(), "QUJDREVGR0g") {
		t.Fatalf("err=%v", err)
	}
}
