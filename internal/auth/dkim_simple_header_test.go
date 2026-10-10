package auth

// F5313: RFC 6376 §3.4.1 simple header canonicalization keeps the CRLF;
// signatures come from an independent reference hash input.

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestDKIM_SimpleHeaderCanon_F5313(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("Hi\r\n")
	bh := sha256.Sum256(body)
	hdrs := map[string][]string{"from": {"a@example.com"}, "subject": {"x"}}
	sign := func(c, h, data string) string {
		tags := "v=1; a=rsa-sha256;" + c + " d=example.com; s=sel; h=" + h + "; bh=" + base64.StdEncoding.EncodeToString(bh[:]) + "; b="
		d := sha256.Sum256([]byte(data + "DKIM-Signature: " + tags))
		s, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, d[:])
		if err != nil {
			t.Fatal(err)
		}
		return tags + base64.StdEncoding.EncodeToString(s)
	}
	v := NewDKIMVerifier(newMockDNSResolver())
	v.cache.set("sel", "example.com", &key.PublicKey, nil)
	// 1. Explicit c=simple/simple, one header.
	if r, _, err := v.Verify(hdrs, body, sign(" c=simple/simple;", "from", "from: a@example.com\r\n")); r != DKIMPass {
		t.Fatalf("simple one header: %v %v", r, err)
	}
	// 2. No c= tag (default simple/simple), two headers.
	if r, _, err := v.Verify(hdrs, body, sign("", "from:subject", "from: a@example.com\r\nsubject: x\r\n")); r != DKIMPass {
		t.Fatalf("default canon two headers: %v %v", r, err)
	}
	// 3. Oversigned h=from:from contributes the one instance only.
	if r, _, err := v.Verify(hdrs, body, sign(" c=simple/simple;", "from:from", "from: a@example.com\r\n")); r != DKIMPass {
		t.Fatalf("oversigned: %v %v", r, err)
	}
	// 4. Tampered header still fails.
	bad := map[string][]string{"from": {"b@example.com"}}
	if r, _, _ := v.Verify(bad, body, sign(" c=simple/simple;", "from", "from: a@example.com\r\n")); r == DKIMPass {
		t.Fatal("tampered header passed")
	}
}
