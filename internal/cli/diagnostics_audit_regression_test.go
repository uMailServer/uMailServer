package cli

// Regression tests for diagnostics findings F4965-F4969 (evidence-audit,
// ledger .temp_files/ledger_internal_cli.md). All hermetic: loopback port-0
// listeners or fake conns through the Diagnostics.dial seam, and the fake
// DNS resolver from diagnostics_rbl_test.go.
//   F4965 STARTTLS on 587 was probed with implicit TLS (always false).
//   F4966 checkSMTPTLS read the SMTP conversation with no deadline.
//   F4967 multiple SPF records (RFC 7208 permerror) reported as pass.
//   F4968 multiple DMARC records (RFC 7489: no policy) reported as pass.
//   F4969 a non-220 (554) port-25 greeting reported as reachable, no issue.

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

// rg4965Server is a correctly configured submission server: plaintext 220
// greeting, EHLO advertising STARTTLS, STARTTLS upgrade.
var (
	rg4965Mu    sync.Mutex
	rg4965First []byte
)

func rg4965Server(t *testing.T) string {
	t.Helper()
	cert, err := generateTestCert()
	if err != nil {
		t.Fatal(err)
	}
	srvTLS := &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				fmt.Fprintf(conn, "220 test.smtp ESMTP\r\n")
				br := bufio.NewReader(conn)
				if b, err := br.Peek(1); err == nil {
					rg4965Mu.Lock()
					rg4965First = append(rg4965First, b[0])
					rg4965Mu.Unlock()
				}
				sc := bufio.NewScanner(br)
				for sc.Scan() {
					up := strings.ToUpper(sc.Text())
					switch {
					case strings.HasPrefix(up, "EHLO"):
						fmt.Fprintf(conn, "250-test.smtp\r\n250 STARTTLS\r\n")
					case strings.HasPrefix(up, "STARTTLS"):
						fmt.Fprintf(conn, "220 Go ahead\r\n")
						tc := tls.Server(conn, srvTLS)
						if tc.Handshake() != nil {
							return
						}
						ts := bufio.NewScanner(tc)
						for ts.Scan() {
							u := strings.ToUpper(ts.Text())
							if strings.HasPrefix(u, "QUIT") {
								fmt.Fprintf(tc, "221 Bye\r\n")
								return
							}
							fmt.Fprintf(tc, "250 OK\r\n")
						}
						return
					case strings.HasPrefix(up, "QUIT"):
						fmt.Fprintf(conn, "221 Bye\r\n")
						return
					default:
						fmt.Fprintf(conn, "250 OK\r\n")
					}
				}
			}(c)
		}
	}()
	return ln.Addr().String()
}

func rg4965Diag(addr string) *Diagnostics {
	d := newTestDiagnostics()
	d.dial = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout(network, addr, timeout)
	}
	return d
}

// Control: the same server passes the existing STARTTLS check (CheckTLS path),
// so the server is a correct STARTTLS server and the seam works.
func TestDiagnosticsRegressionF4965Control(t *testing.T) {
	d := rg4965Diag(rg4965Server(t))
	res, err := d.checkSMTPTLS("mail.test")
	if err != nil || !res.Valid {
		t.Fatalf("INVALID control: checkSMTPTLS err=%v res=%+v", err, res)
	}
	t.Logf("control OK: checkSMTPTLS Valid=%v (%s)", res.Valid, res.Message)
}

func TestDiagnosticsRegressionF4965STARTTLSDetected(t *testing.T) {
	d := rg4965Diag(rg4965Server(t))
	res, issues := d.checkSMTPConnectivity("mail.test")
	t.Logf("EXPECTED: Reachable=true STARTTLS=true")
	t.Logf("ACTUAL:   Reachable=%v STARTTLS=%v msg=%q issues=%v", res.Reachable, res.STARTTLS, res.Message, issues)
	if !res.Reachable {
		t.Fatalf("INVALID: port 25 not reachable through seam")
	}
	rg4965Mu.Lock()
	t.Logf("first byte the server received per conversation: % x (0x16 = TLS ClientHello sent to a plaintext SMTP greeting)", rg4965First)
	rg4965Mu.Unlock()
	if !res.STARTTLS {
		t.Fatalf("F4965: STARTTLS reported unavailable on a server that advertises and completes STARTTLS on 587")
	}
}

// rg4966Conn models a peer that accepts the TCP connection and never sends a
// byte. A Read with a deadline armed returns a timeout at once (fake clock:
// the deadline "expires"); a Read with NO deadline would block forever on a
// real socket, so it is recorded as a violation and failed instead of hanging.
type rg4966Conn struct {
	mu         sync.Mutex
	deadline   bool
	violations int
}

type rg4966Timeout struct{}

func (rg4966Timeout) Error() string   { return "i/o timeout (fake deadline expired)" }
func (rg4966Timeout) Timeout() bool   { return true }
func (rg4966Timeout) Temporary() bool { return true }

func (c *rg4966Conn) Read(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deadline {
		return 0, rg4966Timeout{}
	}
	c.violations++
	return 0, errors.New("read without deadline: would block forever on a silent peer")
}
func (c *rg4966Conn) Write(b []byte) (int, error)        { return len(b), nil }
func (c *rg4966Conn) Close() error                       { return nil }
func (c *rg4966Conn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (c *rg4966Conn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (c *rg4966Conn) SetDeadline(t time.Time) error      { return c.SetReadDeadline(t) }
func (c *rg4966Conn) SetWriteDeadline(t time.Time) error { return nil }
func (c *rg4966Conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = !t.IsZero()
	c.mu.Unlock()
	return nil
}

func rg4966Diag() (*Diagnostics, *[]*rg4966Conn) {
	var conns []*rg4966Conn
	d := newTestDiagnostics()
	d.dial = func(network, address string, timeout time.Duration) (net.Conn, error) {
		c := &rg4966Conn{}
		conns = append(conns, c)
		return c, nil
	}
	return d, &conns
}

// Control: the port-25 greeting read in checkSMTPConnectivity arms a deadline,
// so a silent peer is reported as such and nothing reads unbounded.
func TestDiagnosticsRegressionF4966Control(t *testing.T) {
	d, conns := rg4966Diag()
	res, issues := d.checkSMTPConnectivity("mail.test")
	if len(*conns) == 0 || (*conns)[0].violations != 0 || res.Message != "Could not read SMTP greeting" {
		t.Fatalf("INVALID control: conns=%d res=%+v issues=%v", len(*conns), res, issues)
	}
	t.Logf("control OK: port-25 greeting read is bounded (%s)", res.Message)
}

func TestDiagnosticsRegressionF4966SMTPTLSBounded(t *testing.T) {
	d, conns := rg4966Diag()
	_, err := d.checkSMTPTLS("mail.test")
	v := 0
	for _, c := range *conns {
		v += c.violations
	}
	t.Logf("EXPECTED: every read on the 587 conversation has a deadline (violations=0), err=timeout")
	t.Logf("ACTUAL:   violations=%d err=%v", v, err)
	if err == nil {
		t.Fatalf("INVALID: silent peer produced no error")
	}
	if v > 0 {
		t.Fatalf("F4966: checkSMTPTLS reads the SMTP conversation with no deadline; a peer that accepts and stays silent hangs the CLI forever")
	}
}

func rg4967SPF(t *testing.T, records ...string) DNSCheckResult {
	t.Helper()
	var answers [][]byte
	for _, r := range records {
		answers = append(answers, spfTXTAnswer(r)...)
	}
	addr := startFakeRBLServer(t, func(qname string, qtype uint16) (int, [][]byte) {
		if qname == "example.com" && qtype == spfTypeTXT {
			return rblRCODENoError, answers
		}
		return rblRCODENXDomain, nil
	})
	withFakeRBLResolver(t, addr)
	d := NewDiagnostics(&config.Config{Server: config.ServerConfig{Hostname: "mail.example.com"}})
	return d.checkSPF("example.com")
}

// Control: one authorizing SPF record (plus an unrelated TXT) passes.
func TestDiagnosticsRegressionF4967Control(t *testing.T) {
	r := rg4967SPF(t, "google-site-verification=abc", "v=spf1 mx -all")
	if r.Status != "pass" {
		t.Fatalf("INVALID control: single SPF record status=%q (%s)", r.Status, r.Message)
	}
	t.Logf("control OK: single record -> %s", r.Status)
}

// RFC 7208 4.5: more than one "v=spf1" record => permerror; receivers apply no
// SPF policy, so this server is NOT authorized.
func TestDiagnosticsRegressionF4967MultipleRecords(t *testing.T) {
	r := rg4967SPF(t, "v=spf1 mx -all", "v=spf1 include:_spf.other.example ~all")
	t.Logf("EXPECTED: status=fail (RFC 7208 4.5 permerror: multiple SPF records)")
	t.Logf("ACTUAL:   status=%s msg=%q", r.Status, r.Message)
	if r.Status == "pass" {
		t.Fatalf("F4967: two v=spf1 records (permerror) reported as pass")
	}
}

func rg4968DMARC(t *testing.T, records ...string) DNSCheckResult {
	t.Helper()
	var answers [][]byte
	for _, r := range records {
		answers = append(answers, spfTXTAnswer(r)...)
	}
	addr := startFakeRBLServer(t, func(qname string, qtype uint16) (int, [][]byte) {
		if qname == "_dmarc.example.com" && qtype == spfTypeTXT {
			return rblRCODENoError, answers
		}
		return rblRCODENXDomain, nil
	})
	withFakeRBLResolver(t, addr)
	d := NewDiagnostics(&config.Config{Server: config.ServerConfig{Hostname: "mail.example.com"}})
	return d.checkDMARC("example.com")
}

func TestDiagnosticsRegressionF4968Control(t *testing.T) {
	r := rg4968DMARC(t, "v=DMARC1; p=reject")
	if r.Status != "pass" {
		t.Fatalf("INVALID control: single DMARC record status=%q (%s)", r.Status, r.Message)
	}
	t.Logf("control OK: single record -> %s", r.Status)
}

// RFC 7489 6.6.3: if the _dmarc TXT set holds more than one DMARC record,
// policy discovery terminates and DMARC is not applied at all.
func TestDiagnosticsRegressionF4968MultipleRecords(t *testing.T) {
	r := rg4968DMARC(t, "v=DMARC1; p=reject", "v=DMARC1; p=none")
	t.Logf("EXPECTED: status!=pass (RFC 7489 6.6.3: multiple records => no DMARC policy)")
	t.Logf("ACTUAL:   status=%s msg=%q", r.Status, r.Message)
	if r.Status == "pass" {
		t.Fatalf("F4968: two DMARC records (no policy applied by receivers) reported as pass")
	}
}

func rg4969Server(t *testing.T, greeting string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			fmt.Fprintf(c, "%s\r\n", greeting)
			_ = c.Close()
		}
	}()
	return ln.Addr().String()
}

func rg4969Check(t *testing.T, greeting string) (*SMTPCheckResult, []string) {
	addr := rg4969Server(t, greeting)
	d := newTestDiagnostics()
	d.dial = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout(network, addr, timeout)
	}
	return d.checkSMTPConnectivity("mail.test")
}

// Control: a 220 greeting yields no port-25 greeting issue.
func TestDiagnosticsRegressionF4969Control(t *testing.T) {
	res, issues := rg4969Check(t, "220 mail.test ESMTP")
	for _, i := range issues {
		if i == "SMTP: Could not read server greeting" {
			t.Fatalf("INVALID control: %v", issues)
		}
	}
	if !res.Reachable {
		t.Fatalf("INVALID control: unreachable")
	}
	t.Logf("control OK: 220 greeting -> issues=%v", issues)
}

// RFC 5321 3.1: a 554 greeting means the server refuses all transactions.
func TestDiagnosticsRegressionF4969RefusalGreeting(t *testing.T) {
	res, issues := rg4969Check(t, "554 mail.test no SMTP service here")
	t.Logf("EXPECTED: an issue reporting the 554 refusal on port 25")
	t.Logf("ACTUAL:   reachable=%v msg=%q issues=%v", res.Reachable, res.Message, issues)
	for _, i := range issues {
		if len(i) >= 4 && (rgContains4969(i, "554") || rgContains4969(i, "greeting")) {
			return
		}
	}
	t.Fatalf("F4969: a 554 'no service' greeting on port 25 is reported as reachable with no issue")
}

func rgContains4969(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
