package main

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// TestQuickstartHelperProcess runs cmdQuickstart in a child process (it calls
// os.Exit), driven by r291RunQuickstart.
func TestQuickstartHelperProcess(t *testing.T) {
	if os.Getenv("UMAIL_QS_HELPER") != "1" {
		t.Skip("helper process")
	}
	cmdQuickstart(strings.Split(os.Getenv("UMAIL_QS_ARGS"), "|"))
	os.Exit(0)
}

func r291RunQuickstart(t *testing.T, email string) (dataDir string, d *db.DomainData) {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "umailserver.yaml")
	dataDir = filepath.Join(dir, "data")
	cmd := exec.Command(os.Args[0], "-test.run=^TestQuickstartHelperProcess$")
	cmd.Env = append(os.Environ(), "UMAIL_QS_HELPER=1",
		"UMAIL_QS_ARGS="+strings.Join([]string{email, "--data-dir", dataDir, "--config", cfg}, "|"),
		"HOME="+dir)
	cmd.Stdin = strings.NewReader("Quick-Start-Pass-1\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("quickstart failed: %v\n%s", err, out)
	}
	database, err := db.Open(filepath.Join(dataDir, "umailserver.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, domain, _ := strings.Cut(email, "@")
	d, err = database.GetDomain(domain)
	if err != nil {
		t.Fatal(err)
	}
	return dataDir, d
}

// F7650: quickstart must store the DKIM private key on the domain record,
// because the outbound queue signs with it (domain add and the REST API do).
func TestQuickstartStoresDKIMPrivateKey(t *testing.T) {
	dataDir, d := r291RunQuickstart(t, "qs@dkim.test")
	if d.DKIMSelector != "default" || d.DKIMPublicKey == "" {
		t.Fatalf("selector=%q public key=%d bytes", d.DKIMSelector, len(d.DKIMPublicKey))
	}
	if d.DKIMPrivateKey == "" {
		t.Fatal("domain has no DKIM private key: outbound mail would be sent unsigned")
	}
	// the stored key parses and is the pair of the published public key
	block, _ := pem.Decode([]byte(d.DKIMPrivateKey))
	if block == nil {
		t.Fatal("stored private key is not PEM")
	}
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("stored private key does not parse: %v", err)
	}
	der, err := base64.StdEncoding.DecodeString(d.DKIMPublicKey)
	if err != nil {
		t.Fatalf("public key is not base64: %v", err)
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		t.Fatalf("public key does not parse: %v", err)
	}
	if !priv.PublicKey.Equal(pub.(*rsa.PublicKey)) {
		t.Fatal("stored private key does not match the published public key")
	}
	// the key file written for operators holds the same key
	onDisk, err := os.ReadFile(filepath.Join(dataDir, "dkim", "dkim.test.private.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != d.DKIMPrivateKey {
		t.Fatal("database key differs from the key file")
	}
}
