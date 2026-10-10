package queue

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
)

// stsResolver is a DNS stub for MTA-STS: TXT id per domain, public IP for the
// policy host so the SSRF check passes.
type stsResolver struct {
	mu  sync.Mutex
	txt string
}

func (r *stsResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.HasPrefix(name, "_mta-sts.") && r.txt != "" {
		return []string{r.txt}, nil
	}
	return nil, &net.DNSError{IsNotFound: true}
}
func (r *stsResolver) LookupIP(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}
func (r *stsResolver) LookupMX(context.Context, string) ([]*net.MX, error) { return nil, nil }

// stsManager returns a manager whose MTA-STS validator fetches body as the
// policy of every domain.
func stsManager(t *testing.T, policy string, cert *tls.Certificate) (*Manager, *int32) {
	t.Helper()
	m, dials := newPoolTestManager(cert)
	m.logger = slog.Default()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(policy))
	}))
	t.Cleanup(srv.Close)
	v := auth.NewMTASTSValidator(&stsResolver{txt: "v=STSv1; id=1"})
	v.SetHTTPClient(&http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test server
			DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, n, srv.Listener.Addr().String())
			},
		},
	})
	m.mtastsValidator = v
	return m, dials
}

func stsPolicy(mode, mx string) string {
	return "version: STSv1\nmode: " + mode + "\nmx: " + mx + "\nmax_age: 86400\n"
}

// Enforce mode must never fall back to plaintext when STARTTLS is refused (502).
func TestR141_EnforceNoPlaintextFallback(t *testing.T) {
	m, _ := stsManager(t, stsPolicy("enforce", poolTestMX), nil)
	err := deliverPoolTest(m, "b@example.test")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS required") {
		t.Fatalf("enforce + no STARTTLS must fail, got %v", err)
	}
	var pol *tlsPolicyError
	if !errors.As(err, &pol) {
		t.Fatalf("expected tlsPolicyError, got %T", err)
	}
	if got := m.TLSFailureCounts()["example.test|starttls-not-supported"]; got != 1 {
		t.Fatalf("TLS-RPT counter = %d", got)
	}
}

// Enforce mode must reject a certificate that does not validate for the MX.
func TestR141_EnforceRejectsUntrustedCert(t *testing.T) {
	m, _ := stsManager(t, stsPolicy("enforce", poolTestMX), nil)
	m.dialSMTP = starttlsDialer(selfSignedMXCert(t))
	if err := deliverPoolTest(m, "b@example.test"); err == nil {
		t.Fatal("enforce + self-signed certificate must fail")
	}
}

// Enforce mode delivers when the certificate is trusted for the MX name.
func TestR141_EnforceAcceptsTrustedCert(t *testing.T) {
	m, _ := stsManager(t, stsPolicy("enforce", "*.example.test"), nil)
	cert := selfSignedMXCert(t)
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	m.tlsRootCAs = x509.NewCertPool()
	m.tlsRootCAs.AddCert(leaf)
	m.dialSMTP = starttlsDialer(cert)
	if err := deliverPoolTest(m, "b@example.test"); err != nil {
		t.Fatalf("trusted cert + matching mx pattern must deliver: %v", err)
	}
}

// MX hosts outside the policy are skipped (case-insensitive, one-label wildcard).
func TestR141_EnforceMXPatternMismatch(t *testing.T) {
	m, _ := stsManager(t, stsPolicy("enforce", "*.other.test"), nil)
	err := deliverPoolTest(m, "b@example.test")
	if err == nil || !strings.Contains(err.Error(), "MTA-STS policy violation") {
		t.Fatalf("expected policy violation, got %v", err)
	}
	m2, _ := stsManager(t, stsPolicy("enforce", "*.TEST"), nil) // wildcard = exactly one label
	if err := deliverPoolTest(m2, "b@example.test"); err == nil || !strings.Contains(err.Error(), "policy violation") {
		t.Fatalf("two-label host must not match *.test: %v", err)
	}
}

// Testing mode delivers opportunistically but records failures.
func TestR141_TestingModeDeliversAndCounts(t *testing.T) {
	m, _ := stsManager(t, stsPolicy("testing", poolTestMX), nil)
	if err := deliverPoolTest(m, "b@example.test"); err != nil {
		t.Fatalf("testing mode must deliver: %v", err)
	}
	if got := m.TLSFailureCounts()["example.test|starttls-not-supported"]; got != 1 {
		t.Fatalf("testing-mode failure not counted: %v", m.TLSFailureCounts())
	}
	m2, _ := stsManager(t, stsPolicy("testing", poolTestMX), nil)
	m2.dialSMTP = starttlsDialer(selfSignedMXCert(t))
	if err := deliverPoolTest(m2, "b@example.test"); err != nil {
		t.Fatalf("testing mode must deliver over untrusted TLS: %v", err)
	}
	if got := m2.TLSFailureCounts()["example.test|certificate-not-trusted"]; got != 1 {
		t.Fatalf("untrusted cert not counted: %v", m2.TLSFailureCounts())
	}
}

// F6022 with MTA-STS: a pooled opportunistic (unverified) TLS connection must
// not carry an enforce-mode delivery.
func TestR141_EnforceRefusesPooledUnverifiedTLS(t *testing.T) {
	m, dials := stsManager(t, stsPolicy("enforce", poolTestMX), selfSignedMXCert(t))
	seed, err := m.createMXConn(poolTestMX)
	if err != nil {
		t.Fatal(err)
	}
	m.releaseMXConn(poolTestMX, seed, true)
	before := atomic.LoadInt32(dials)
	// The harness' fresh dial is TLS from the start with an unverified client
	// config, so the delivery itself may succeed; what matters is that the
	// pooled unverified session was discarded and a new connection dialed.
	_ = deliverPoolTest(m, "b@example.test")
	if atomic.LoadInt32(dials) == before {
		t.Fatal("pooled unverified TLS session was reused for an enforce delivery")
	}
	for _, c := range pooledClients(m) {
		if c == seed {
			t.Fatal("unverified seed connection still pooled")
		}
	}
}

// DANE stays advisory without a DNSSEC-authenticated resolver.
func TestR141_DANEAdvisoryWithoutSecureHook(t *testing.T) {
	m := &Manager{logger: slog.Default()}
	m.daneValidator = auth.NewDANEValidator(&tlsaStub{})
	if err := m.checkDANE("mx.example.test", tls.ConnectionState{}); err != nil {
		t.Fatalf("DANE without validating resolver must not fail delivery: %v", err)
	}
	m.SetDANESecureHook(func(string) bool { return true })
	// Authenticated TLSA data that cannot match the (absent) peer certificate fails.
	if err := m.checkDANE("mx.example.test", tls.ConnectionState{}); err == nil {
		t.Fatal("DNSSEC-authenticated TLSA mismatch must fail delivery")
	}
}

type tlsaStub struct{ stsResolver }

func (*tlsaStub) LookupTLSA(string) ([]*auth.TLSARecord, error) {
	return []*auth.TLSARecord{{Usage: auth.TLSAUsageDANEEE, Selector: 0, MatchingType: 1, Certificate: make([]byte, 32)}}, nil
}

// starttlsDialer serves a peer that upgrades via STARTTLS with cert.
func starttlsDialer(cert *tls.Certificate) func(string) (net.Conn, error) {
	return func(string) (net.Conn, error) {
		c, s := net.Pipe()
		fakeSTARTTLSPeer(s, cert)
		return c, nil
	}
}

// fakeSTARTTLSPeer is an SMTP peer that offers STARTTLS and, after the 220
// reply, continues the session over TLS with cert.
func fakeSTARTTLSPeer(conn net.Conn, cert *tls.Certificate) {
	go func() {
		defer func() { _ = conn.Close() }()
		var c net.Conn = conn
		buf := make([]byte, 0, 512)
		readLine := func() (string, bool) {
			buf = buf[:0]
			b := make([]byte, 1)
			for {
				if _, err := c.Read(b); err != nil {
					return "", false
				}
				buf = append(buf, b[0])
				if b[0] == '\n' {
					return strings.ToUpper(strings.TrimSpace(string(buf))), true
				}
			}
		}
		say := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		say("220 fake ESMTP")
		for {
			cmd, ok := readLine()
			if !ok {
				return
			}
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250-fake")
				say("250 STARTTLS")
			case cmd == "STARTTLS":
				say("220 go ahead")
				c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{*cert}})
			case cmd == "DATA":
				say("354 go ahead")
				for {
					l, ok := readLine()
					if !ok {
						return
					}
					if l == "." {
						break
					}
				}
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
}
