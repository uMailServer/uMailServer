package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// F6210: DKIM TXT value is base64 only (no PEM armor/newlines) and split into
// <=255-byte quoted strings that concatenate to the exact record.
func TestR139_DKIMTXTChunks(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	dir := t.TempDir()
	kp := filepath.Join(dir, "k.pem")
	if err := generateDKIMKey(kp); err != nil {
		t.Fatal(err)
	}
	pemData, _ := os.ReadFile(kp + ".pub")
	b64, err := dkimPublicKeyB64(pemData)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(b64, "-\n ") {
		t.Fatalf("p= value contains PEM armor/whitespace: %q", b64)
	}
	if _, err := base64.StdEncoding.DecodeString(b64); err != nil {
		t.Fatal(err)
	}
	_ = der
	rec := dkimTXT(b64)
	var joined strings.Builder
	for _, part := range strings.Split(rec, `" "`) {
		part = strings.Trim(part, `"`)
		if len(part) > 255 {
			t.Fatalf("chunk of %d bytes exceeds 255", len(part))
		}
		joined.WriteString(part)
	}
	if joined.String() != "v=DKIM1; k=rsa; p="+b64 {
		t.Fatal("chunks do not reassemble to the record")
	}
	if !strings.Contains(rec, `" "`) {
		t.Fatal("2048-bit key record must be split")
	}
}

// F6211: DKIM private key is 0600 even if a looser file already exists.
func TestR139_DKIMKeyPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	kp := filepath.Join(t.TempDir(), "k.pem")
	_ = os.WriteFile(kp, []byte("old"), 0o666)
	if err := generateDKIMKey(kp); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(kp)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode %v, want 0600", st.Mode().Perm())
	}
}

func TestR139_ParseEmailAndConfig(t *testing.T) {
	for _, bad := range []string{"@example.com", "a@", "a@b@c", "noat", "a b@c.d"} {
		if _, _, ok := parseEmail(bad); ok {
			t.Errorf("parseEmail(%q) accepted", bad)
		}
	}
	if l, d, ok := parseEmail("a@example.com"); !ok || l != "a" || d != "example.com" {
		t.Fatal("valid email rejected")
	}
	if requireConfigFile(filepath.Join(t.TempDir(), "nope.yaml")) == nil {
		t.Fatal("missing explicit config must be an error")
	}
	if requireConfigFile("") != nil {
		t.Fatal("empty path is not explicit")
	}
}

func TestR139_ProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("own pid must be alive")
	}
	if processAlive(0) || processAlive(-1) {
		t.Fatal("non-positive pid must not be alive")
	}
}
