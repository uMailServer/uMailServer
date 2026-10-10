package auth

// Round 88 regressions (F5705, F5706): DKIM x= expiry and c=simple field
// name case. Hash inputs come from an independent reference construction.

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

type r88DKIM struct {
	key  *rsa.PrivateKey
	body []byte
	bh   string
}

func newR88DKIM(t *testing.T) *r88DKIM {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("Hi\r\n")
	h := sha256.Sum256(body)
	return &r88DKIM{key, body, base64.StdEncoding.EncodeToString(h[:])}
}

// verify signs hashedFields + the DKIM-Signature field (b= empty, as given by
// sigField) and verifies headers against it.
func (f *r88DKIM) verify(t *testing.T, headers map[string][]string, tags, hashedFields, sigField string) DKIMResult {
	t.Helper()
	dg := sha256.Sum256([]byte(hashedFields + sigField))
	s, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, dg[:])
	if err != nil {
		t.Fatal(err)
	}
	v := NewDKIMVerifier(newMockDNSResolver())
	v.cache.set("sel", "example.com", &f.key.PublicKey, nil)
	res, _, _ := v.Verify(headers, f.body, tags+base64.StdEncoding.EncodeToString(s))
	return res
}

func TestRound88_F5705_ExpiredSignature(t *testing.T) {
	f := newR88DKIM(t)
	hdrs := map[string][]string{"from": {"a@example.com"}}
	for name, tc := range map[string]struct {
		x    string
		want DKIMResult
	}{
		"no x=":       {"", DKIMPass},
		"future x=":   {" x=4102444800;", DKIMPass},
		"expired x=":  {" x=1000000000;", DKIMFail},
		"x=1 expired": {" x=1;", DKIMFail},
	} {
		tags := "v=1; a=rsa-sha256; c=relaxed/relaxed; d=example.com; s=sel;" + tc.x + " h=from; bh=" + f.bh + "; b="
		field := "dkim-signature:" + tags
		// reference relaxed form: single spaces, no space after the colon
		if got := f.verify(t, hdrs, tags, "from:a@example.com\r\n", field); got != tc.want {
			t.Errorf("%s: got %v want %v", name, got, tc.want)
		}
	}
}

func TestRound88_F5706_SimpleKeepsFieldNameCase(t *testing.T) {
	f := newR88DKIM(t)
	tags := "v=1; a=rsa-sha256; c=simple/simple; d=example.com; s=sel; h=from:Subject; bh=" + f.bh + "; b="
	field := "DKIM-Signature: " + tags
	want := "From: a@example.com\r\nSubject: s\r\n"
	// Mixed-case map keys as produced by the SMTP session.
	if got := f.verify(t, map[string][]string{"From": {"a@example.com"}, "Subject": {"s"}}, tags, want, field); got != DKIMPass {
		t.Errorf("mixed-case keys: %v", got)
	}
	// A header written in another case hashes as written and so is rejected
	// when the signer hashed a different spelling.
	if got := f.verify(t, map[string][]string{"FROM": {"a@example.com"}, "Subject": {"s"}}, tags, want, field); got == DKIMPass {
		t.Error("different field-name case passed under c=simple")
	}
	// Lowercase keys (existing callers) still verify against lowercase input.
	if got := f.verify(t, map[string][]string{"from": {"a@example.com"}, "subject": {"s"}}, tags, "from: a@example.com\r\nsubject: s\r\n", field); got != DKIMPass {
		t.Errorf("lowercase keys: %v", got)
	}
	// Relaxed is unaffected by key case.
	rtags := "v=1; a=rsa-sha256; c=relaxed/relaxed; d=example.com; s=sel; h=from; bh=" + f.bh + "; b="
	if got := f.verify(t, map[string][]string{"From": {"a@example.com"}}, rtags, "from:a@example.com\r\n", "dkim-signature:"+rtags); got != DKIMPass {
		t.Errorf("relaxed: %v", got)
	}
}
