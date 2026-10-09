package auth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/mail"
	"strings"
	"testing"
)

// Regression tests for F4890/F4891/F4892 (and F4889): the RFC 8463
// Appendix A message, keys and signatures, verified offline through the
// public Verify API with the keys pre-seeded into the verifier cache.

const rfc8463EdSeed = "nWGxne/9WmC6hEr0kuwsxERJxWl7MmkZcDusAxyuf2A="
const rfc8463EdPub = "11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="

const rfc8463RSAKey = `-----BEGIN RSA PRIVATE KEY-----
MIICXQIBAAKBgQDkHlOQoBTzWRiGs5V6NpP3idY6Wk08a5qhdR6wy5bdOKb2jLQi
Y/J16JYi0Qvx/byYzCNb3W91y3FutACDfzwQ/BC/e/8uBsCR+yz1Lxj+PL6lHvqM
KrM3rG4hstT5QjvHO9PzoxZyVYLzBfO2EeC3Ip3G+2kryOTIKT+l/K4w3QIDAQAB
AoGAH0cxOhFZDgzXWhDhnAJDw5s4roOXN4OhjiXa8W7Y3rhX3FJqmJSPuC8N9vQm
6SVbaLAE4SG5mLMueHlh4KXffEpuLEiNp9Ss3O4YfLiQpbRqE7Tm5SxKjvvQoZZe
zHorimOaChRL2it47iuWxzxSiRMv4c+j70GiWdxXnxe4UoECQQDzJB/0U58W7RZy
6enGVj2kWF732CoWFZWzi1FicudrBFoy63QwcowpoCazKtvZGMNlPWnC7x/6o8Gc
uSe0ga2xAkEA8C7PipPm1/1fTRQvj1o/dDmZp243044ZNyxjg+/OPN0oWCbXIGxy
WvmZbXriOWoSALJTjExEgraHEgnXssuk7QJBALl5ICsYMu6hMxO73gnfNayNgPxd
WFV6Z7ULnKyV7HSVYF0hgYOHjeYe9gaMtiJYoo0zGN+L3AAtNP9huqkWlzECQE1a
licIeVlo1e+qJ6Mgqr0Q7Aa7falZ448ccbSFYEPD6oFxiOl9Y9se9iYHZKKfIcst
o7DUw1/hz2Ck4N5JrgUCQQCyKveNvjzkkd8HjYs0SwM0fPjK16//5qDZ2UiDGnOe
uEzxBDAr518Z8VFbR41in3W4Y3yCDgQlLlcETrS+zYcL
-----END RSA PRIVATE KEY-----`

var rfc8463Message = strings.Join([]string{
	"DKIM-Signature: v=1; a=ed25519-sha256; c=relaxed/relaxed;",
	" d=football.example.com; i=@football.example.com;",
	" q=dns/txt; s=brisbane; t=1528637909; h=from : to :",
	" subject : date : message-id : from : subject : date;",
	" bh=2jUSOH9NhtVGCQWNr9BrIAPreKQjO6Sn7XIkfJVOzv8=;",
	" b=/gCrinpcQOoIfuHNQIbq4pgh9kyIK3AQUdt9OdqQehSwhEIug4D11Bus",
	" Fa3bT3FY5OsU7ZbnKELq+eXdp1Q1Dw==",
	"DKIM-Signature: v=1; a=rsa-sha256; c=relaxed/relaxed;",
	" d=football.example.com; i=@football.example.com;",
	" q=dns/txt; s=test; t=1528637909; h=from : to : subject :",
	" date : message-id : from : subject : date;",
	" bh=2jUSOH9NhtVGCQWNr9BrIAPreKQjO6Sn7XIkfJVOzv8=;",
	" b=F45dVWDfMbQDGHJFlXUNB2HKfbCeLRyhDXgFpEL8GwpsRe0IeIixNTe3",
	" DhCVlUrSjV4BwcVcOF6+FF3Zo9Rpo1tFOeS9mPYQTnGdaSGsgeefOsk2Jz",
	" dA+L10TeYt9BgDfQNZtKdN1WO//KgIqXP7OdEFE4LjFYNcUxZQ4FADY+8=",
	"From: Joe SixPack <joe@football.example.com>",
	"To: Suzie Q <suzie@shopping.example.net>",
	"Subject: Is dinner ready?",
	"Date: Fri, 11 Jul 2003 21:00:37 -0700 (PDT)",
	"Message-ID: <20030712040037.46341.5F8J@football.example.com>",
	"",
	"Hi.",
	"",
	"We lost the game.  Are you hungry yet?",
	"",
	"Joe.",
	"",
}, "\r\n")

func rfc8463Verifier(t *testing.T) *DKIMVerifier {
	t.Helper()
	seed, _ := base64.StdEncoding.DecodeString(rfc8463EdSeed)
	edPub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if want, _ := base64.StdEncoding.DecodeString(rfc8463EdPub); !edPub.Equal(ed25519.PublicKey(want)) {
		t.Fatal("RFC 8463 ed25519 seed/public key mismatch")
	}
	blk, _ := pem.Decode([]byte(rfc8463RSAKey))
	rsaKey, err := x509.ParsePKCS1PrivateKey(blk.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	v := NewDKIMVerifier(newMockDNSResolver())
	v.cache.set("brisbane", "football.example.com", nil, edPub)
	v.cache.set("test", "football.example.com", &rsaKey.PublicKey, nil)
	return v
}

func rfc8463Parse(t *testing.T, raw string) (map[string][]string, []byte) {
	t.Helper()
	m, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]string(m.Header), []byte(raw[strings.Index(raw, "\r\n\r\n")+4:])
}

func TestDKIMVerify_RFC8463Vector(t *testing.T) {
	hdrs, body := rfc8463Parse(t, rfc8463Message)
	v := rfc8463Verifier(t)
	sigs := hdrs["Dkim-Signature"]
	if len(sigs) != 2 {
		t.Fatalf("expected 2 signatures, got %d", len(sigs))
	}
	for _, val := range sigs {
		res, sig, err := v.Verify(hdrs, body, val)
		if res != DKIMPass {
			t.Errorf("RFC 8463 %s: %s %v", sig.Algorithm, res, err)
		}
	}

	// Tampering with an oversigned header must break both signatures.
	hdrs["Subject"] = []string{"Is dinner burnt?"}
	for _, val := range sigs {
		if res, _, _ := v.Verify(hdrs, body, val); res == DKIMPass {
			t.Errorf("tampered subject accepted")
		}
	}
}

func TestDKIMSign_Ed25519MatchesRFC8463(t *testing.T) {
	// Ed25519 is deterministic: the signer must reproduce the RFC b= value.
	hdrs, body := rfc8463Parse(t, rfc8463Message)
	seed, _ := base64.StdEncoding.DecodeString(rfc8463EdSeed)
	var val string
	for _, s := range hdrs["Dkim-Signature"] {
		if strings.Contains(s, "ed25519-sha256") {
			val = s
		}
	}
	sig, err := parseDKIMSignature(val)
	if err != nil {
		t.Fatal(err)
	}
	data := canonicalizeHeaders(hdrs, sig.SignedHeaders, sig.HeaderCanon) + dkimSignatureFieldForHash(val, sig.HeaderCanon)
	got, _ := signEd25519(ed25519.NewKeyFromSeed(seed), []byte(data))
	if got != sig.Signature {
		t.Errorf("signEd25519 = %s, want %s", got, sig.Signature)
	}
	if bh := computeBodyHash(body, sig.BodyCanon); bh != sig.BodyHash {
		t.Errorf("body hash %s, want %s", bh, sig.BodyHash)
	}
}

func TestDKIMSignVerify_RoundTripRSA(t *testing.T) {
	blk, _ := pem.Decode([]byte(rfc8463RSAKey))
	key, err := x509.ParsePKCS1PrivateKey(blk.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	s := NewDKIMSigner(newMockDNSResolver(), key, "football.example.com", "test")
	hdrs := map[string][]string{"From": {"a@football.example.com"}, "To": {"b@example.net"}, "Subject": {"hi  there"}, "Date": {"Fri, 11 Jul 2003 21:00:37 -0700"}, "Message-Id": {"<x@y>"}}
	body := []byte("Hello\r\n\r\n")
	val, err := s.Sign(hdrs, body)
	if err != nil {
		t.Fatal(err)
	}
	if res, _, err := rfc8463Verifier(t).Verify(hdrs, body, "DKIM-Signature: "+val); res != DKIMPass {
		t.Errorf("sign->verify: %s %v", res, err)
	}
}

func TestCanonicalizeHeaders_BottomUpOversign(t *testing.T) {
	h := map[string][]string{"Received": {"top", "bottom"}, "From": {"x@example.test"}}
	cases := []struct {
		signed []string
		want   string
	}{
		{[]string{"received"}, "received:bottom\r\n"},
		{[]string{"received", "received"}, "received:bottom\r\nreceived:top\r\n"},
		{[]string{"from", "from"}, "from:x@example.test\r\n"},
	}
	for _, c := range cases {
		if got := canonicalizeHeaders(h, c.signed, "relaxed"); got != c.want {
			t.Errorf("%v: got %q want %q", c.signed, got, c.want)
		}
	}
}

func TestCanonicalizeBody_SimpleRFC6376(t *testing.T) {
	if got := string(canonicalizeBody([]byte("Hi\r\n\r\n\r\n"), "simple")); got != "Hi\r\n" {
		t.Errorf("got %q", got)
	}
	if got := computeBodyHash(nil, "simple"); got != "frcCV1k9oG9oKj3dpUqdJg1PxRT2RSN/XKdLCPjaYaY=" {
		t.Errorf("empty simple body hash %s", got)
	}
}
