package server

// Regression tests for F5630 (scripts survive a restart) and F5631 (one
// canonical user key for ManageSieve and delivery). See
// .temp_files/ledger_internal_sieve_manager_go.md, Round 81.

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

func sievePersistCert(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mx.test.com"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"mx.test.com"}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

// sievePersistServer builds a Server on dir (reusable across "restarts").
func sievePersistServer(t *testing.T, dir string) *Server {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Server.DataDir = dir
	cfg.Server.Hostname = "mx.test.com"
	cfg.Database.Path = filepath.Join(dir, "db")
	cfg.Logging.Level = "error"
	cfg.Logging.Output = ""
	cfg.TLS.ACME.Enabled = false
	cfg.ManageSieve.Enabled = true
	cfg.ManageSieve.Bind = "127.0.0.1"
	cfg.ManageSieve.Port = 0
	cfg.TLS.CertFile, cfg.TLS.KeyFile = sievePersistCert(t, dir)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return srv
}

func TestSievePersistSurvivesServerRestart(t *testing.T) {
	dir := t.TempDir()
	srv1 := sievePersistServer(t, dir)
	if err := srv1.startManageSieve(); err != nil {
		t.Fatal(err)
	}
	m := srv1.sieveManager
	if err := m.StoreScript("alice@test.com", "main", "require \"fileinto\"; fileinto \"Work\";"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetActiveScriptByName("alice@test.com", "main"); err != nil {
		t.Fatal(err)
	}
	if err := srv1.Stop(); err != nil {
		t.Logf("stop: %v", err)
	}

	srv2 := sievePersistServer(t, dir)
	defer func() { _ = srv2.Stop() }()
	// Delivery is the first user of the store here: it attaches lazily.
	if got := srv2.activeSieveUser("alice@test.com"); got != "alice@test.com" {
		t.Fatalf("active script after restart: key %q", got)
	}
	if got := srv2.activeSieveUser("bob@test.com"); got != "" {
		t.Fatalf("control: bob has no script, got %q", got)
	}
	if src := srv2.sieveManager.GetScriptSource("alice@test.com", "main"); src != `require "fileinto"; fileinto "Work";` {
		t.Fatalf("source = %q", src)
	}
}

func sievePersistLogin(t *testing.T, addr, login string) {
	t.Helper()
	c, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) // #nosec G402 -- loopback test cert
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	await := func() string {
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if strings.HasPrefix(l, "OK") || strings.HasPrefix(l, "NO") {
				return l
			}
		}
	}
	await() // greeting
	send := func(s string) string { _, _ = fmt.Fprint(c, s); return await() }
	plain := base64.StdEncoding.EncodeToString([]byte("\x00" + login + "\x00pw"))
	for _, cmd := range []string{
		"AUTHENTICATE \"PLAIN\" \"" + plain + "\"\r\n",
		"PUTSCRIPT \"main\" {5+}\r\nkeep;\r\n",
		"SETACTIVE \"main\"\r\n",
	} {
		if l := send(cmd); !strings.HasPrefix(l, "OK") {
			t.Fatalf("%q -> %q", strings.SplitN(cmd, " ", 2)[0], l)
		}
	}
}

// A login typed in another case is the same mailbox: the script must be found
// by delivery, which looks the mailbox up in lowercase.
func TestSievePersistCanonicalKeyThroughProductionWiring(t *testing.T) {
	srv := sievePersistServer(t, t.TempDir())
	defer func() { _ = srv.Stop() }()
	if err := srv.startManageSieve(); err != nil {
		t.Fatal(err)
	}
	srv.manageSieveServer.SetAuthHandler(func(_, pass string) bool { return pass == "pw" })
	addr := srv.manageSieveServer.Addr().String()

	sievePersistLogin(t, addr, "alice@test.com") // control: already canonical
	if srv.activeSieveUser("alice@test.com") == "" {
		t.Fatal("control failed")
	}
	sievePersistLogin(t, addr, "JDoe@Test.com")
	if got := srv.activeSieveUser("jdoe@test.com"); got != "jdoe@test.com" {
		t.Fatalf("delivery key %q; scripts under login: %v", got, srv.sieveManager.ListScripts("JDoe@Test.com"))
	}
	if len(srv.sieveManager.ListScripts("JDoe@Test.com")) != 0 {
		t.Fatal("script also stored under the raw login")
	}
}

func TestCanonicalSieveUser(t *testing.T) {
	ldap := func(login string) string {
		if login == "jdoe" {
			return " JDoe@Example.COM "
		}
		return ""
	}
	for _, tc := range []struct{ login, want string }{
		{"alice@test.com", "alice@test.com"},
		{"Alice@Test.COM", "alice@test.com"},
		{"jdoe", "jdoe@example.com"}, // LDAP uid -> mail attribute
		{"unknown", "unknown"},       // no address known: login kept
		{"", ""},
	} {
		if got := canonicalSieveUser(tc.login, ldap); got != tc.want {
			t.Errorf("canonicalSieveUser(%q) = %q, want %q", tc.login, got, tc.want)
		}
	}
	if got := canonicalSieveUser("jdoe", nil); got != "jdoe" {
		t.Errorf("nil lookup: %q", got)
	}
	if got := canonicalSieveUser("jdoe", func(string) string { return "not-an-address" }); got != "jdoe" {
		t.Errorf("non-address lookup result accepted: %q", got)
	}
}
