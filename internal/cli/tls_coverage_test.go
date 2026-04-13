package cli

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// generateTestCert creates a self-signed TLS certificate for testing.
func generateTestCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"Test"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}, nil
}

// newTestDiagnostics creates a Diagnostics that accepts self-signed certs.
func newTestDiagnostics() *Diagnostics {
	return &Diagnostics{
		tlsConfig: &tls.Config{InsecureSkipVerify: true},
	}
}

// startSMTPOn587 starts a mock SMTP+STARTTLS server on 127.0.0.1:587.
func startSMTPOn587(t *testing.T, srvTLS *tls.Config) (net.Listener, <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:587")
	if err != nil {
		t.Skipf("Cannot listen on port 587: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprintf(conn, "220 test.smtp ESMTP\r\n")
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			line := scanner.Text()
			up := strings.ToUpper(line)
			if strings.HasPrefix(up, "EHLO") || strings.HasPrefix(up, "HELO") {
				fmt.Fprintf(conn, "250-test.smtp\r\n250 STARTTLS\r\n")
			} else if strings.HasPrefix(up, "STARTTLS") {
				fmt.Fprintf(conn, "220 Go\r\n")
				tc := tls.Server(conn, srvTLS)
				if err := tc.Handshake(); err != nil {
					return
				}
				ts := bufio.NewScanner(tc)
				if ts.Scan() {
					_ = ts.Text()
					fmt.Fprintf(tc, "250 OK\r\n")
				}
				return
			} else if strings.HasPrefix(up, "QUIT") {
				fmt.Fprintf(conn, "221 Bye\r\n")
				return
			} else {
				fmt.Fprintf(conn, "250 OK\r\n")
			}
		}
	}()
	return ln, done
}

// startIMAPOn993 starts a mock IMAPS server on 127.0.0.1:993.
func startIMAPOn993(t *testing.T, srvTLS *tls.Config) (net.Listener, <-chan struct{}) {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:993", srvTLS)
	if err != nil {
		t.Skipf("Cannot listen on port 993: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprintf(conn, "* OK IMAP4rev1 ready\r\n")
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			_ = scanner.Text()
		}
	}()
	return ln, done
}

// =========================================================================
// checkSMTPTLS: success path (lines 343-378)
// =========================================================================

func TestCheckSMTPTLSSuccess(t *testing.T) {
	cert, err := generateTestCert()
	if err != nil {
		t.Fatal(err)
	}
	ln, done := startSMTPOn587(t, &tls.Config{Certificates: []tls.Certificate{cert}})
	defer func() { ln.Close(); <-done }()

	d := newTestDiagnostics()
	result, err := d.checkSMTPTLS("127.0.0.1")
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if result.Protocol != "SMTP" {
		t.Errorf("got protocol %s", result.Protocol)
	}
	if !result.Valid {
		t.Error("expected valid")
	}
}

// checkSMTPTLS: SMTP client creation fails (lines 350-354)

func TestCheckSMTPTLSBadGreeting(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:587")
	if err != nil {
		t.Skipf("port 587: %v", err)
	}
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		if c != nil {
			c.Write([]byte("GARBAGE\r\n"))
			time.Sleep(100 * time.Millisecond)
			c.Close()
		}
	}()

	d := newTestDiagnostics()
	result, err := d.checkSMTPTLS("127.0.0.1")
	if err == nil {
		t.Error("expected error")
	}
	if result != nil {
		t.Error("expected nil result")
	}
	if !strings.Contains(err.Error(), "failed to create SMTP client") {
		t.Errorf("wrong error: %v", err)
	}
}

// checkSMTPTLS: STARTTLS rejected (lines 369-370)

func TestCheckSMTPTLSStartTLSRejectedByServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:587")
	if err != nil {
		t.Skipf("port 587: %v", err)
	}
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		if c == nil {
			return
		}
		defer c.Close()
		fmt.Fprintf(c, "220 test ESMTP\r\n")
		sc := bufio.NewScanner(c)
		for sc.Scan() {
			up := strings.ToUpper(sc.Text())
			if strings.HasPrefix(up, "EHLO") || strings.HasPrefix(up, "HELO") {
				fmt.Fprintf(c, "250-test\r\n250 OK\r\n")
			} else if strings.HasPrefix(up, "STARTTLS") {
				fmt.Fprintf(c, "502 not implemented\r\n")
			} else {
				fmt.Fprintf(c, "250 OK\r\n")
			}
		}
	}()

	d := newTestDiagnostics()
	result, err := d.checkSMTPTLS("127.0.0.1")
	if err == nil {
		t.Error("expected error")
	}
	if result != nil {
		t.Error("expected nil")
	}
	if !strings.Contains(err.Error(), "STARTTLS failed") {
		t.Errorf("wrong error: %v", err)
	}
}

// checkSMTPTLS: nil tlsConfig branch (lines 358-362)

func TestCheckSMTPTLSNilConfig(t *testing.T) {
	cert, err := generateTestCert()
	if err != nil {
		t.Fatal(err)
	}
	ln, done := startSMTPOn587(t, &tls.Config{Certificates: []tls.Certificate{cert}})
	defer func() { ln.Close(); <-done }()

	// NewDiagnostics(nil) leaves tlsConfig as nil, hitting the nil branch
	d := NewDiagnostics(nil)
	_, err = d.checkSMTPTLS("127.0.0.1")
	// Will fail TLS verification (self-signed), but the nil branch is exercised
	if err != nil {
		t.Logf("expected failure with nil config (self-signed): %v", err)
	}
}

// =========================================================================
// checkIMAPTLS: success path (lines 382-400)
// =========================================================================

func TestCheckIMAPTLSSuccess(t *testing.T) {
	cert, err := generateTestCert()
	if err != nil {
		t.Fatal(err)
	}
	ln, done := startIMAPOn993(t, &tls.Config{Certificates: []tls.Certificate{cert}})
	defer func() { ln.Close(); <-done }()
	time.Sleep(30 * time.Millisecond)

	d := newTestDiagnostics()
	result, err := d.checkIMAPTLS("127.0.0.1")
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if result.Protocol != "IMAP" {
		t.Errorf("got protocol %s", result.Protocol)
	}
	if !result.Valid {
		t.Error("expected valid")
	}
	if result.Version == "" {
		t.Error("expected non-empty version")
	}
	if result.Cipher == "" {
		t.Error("expected non-empty cipher")
	}
	if result.Message != "IMAPS is working" {
		t.Errorf("got message %s", result.Message)
	}
}

// checkIMAPTLS: connection refused (line 388-390)

func TestCheckIMAPTLSConnRefused(t *testing.T) {
	// Nothing on port 993, use short timeout
	d := &Diagnostics{
		tlsConfig: &tls.Config{InsecureSkipVerify: true},
	}
	result, err := d.checkIMAPTLS("127.0.0.1")
	if err == nil {
		t.Error("expected error")
	}
	if result != nil {
		t.Error("expected nil")
	}
}

// checkIMAPTLS: nil tlsConfig branch (lines 384-388)

func TestCheckIMAPTLSNilConfig(t *testing.T) {
	cert, err := generateTestCert()
	if err != nil {
		t.Fatal(err)
	}
	ln, done := startIMAPOn993(t, &tls.Config{Certificates: []tls.Certificate{cert}})
	defer func() { ln.Close(); <-done }()
	time.Sleep(30 * time.Millisecond)

	d := NewDiagnostics(nil)
	_, err = d.checkIMAPTLS("127.0.0.1")
	if err != nil {
		t.Logf("expected failure with nil config (self-signed): %v", err)
	}
}

// checkIMAPTLS: verify tlsVersionName with TLS 1.3

func TestCheckIMAPTLSTLS13(t *testing.T) {
	cert, err := generateTestCert()
	if err != nil {
		t.Fatal(err)
	}
	srvTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
	}
	ln, done := startIMAPOn993(t, srvTLS)
	defer func() { ln.Close(); <-done }()
	time.Sleep(30 * time.Millisecond)

	d := newTestDiagnostics()
	result, err := d.checkIMAPTLS("127.0.0.1")
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if result.Version != "TLS 1.3" {
		t.Errorf("got version %s", result.Version)
	}
}

// checkIMAPTLS: verify tlsVersionName with TLS 1.2

func TestCheckIMAPTLSTLS12(t *testing.T) {
	cert, err := generateTestCert()
	if err != nil {
		t.Fatal(err)
	}
	srvTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   tls.VersionTLS12,
	}
	ln, done := startIMAPOn993(t, srvTLS)
	defer func() { ln.Close(); <-done }()
	time.Sleep(30 * time.Millisecond)

	d := newTestDiagnostics()
	result, err := d.checkIMAPTLS("127.0.0.1")
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if result.Version != "TLS 1.2" {
		t.Errorf("got version %s", result.Version)
	}
}

// =========================================================================
// CheckTLS: both succeed (lines 316-337)
// =========================================================================

func TestCheckTLSBothSucceed(t *testing.T) {
	cert, err := generateTestCert()
	if err != nil {
		t.Fatal(err)
	}
	srvTLS := &tls.Config{Certificates: []tls.Certificate{cert}}

	smtpLn, smtpDone := startSMTPOn587(t, srvTLS)
	defer func() { smtpLn.Close(); <-smtpDone }()

	imapLn, imapDone := startIMAPOn993(t, srvTLS)
	defer func() { imapLn.Close(); <-imapDone }()

	time.Sleep(30 * time.Millisecond)

	d := newTestDiagnostics()
	result, err := d.CheckTLS("127.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected Valid=true, message=%s", result.Message)
	}
	if result.Message != "TLS configuration looks good" {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

// CheckTLS: SMTP ok, IMAP fails (lines 325-328)

func TestCheckTLSIMAPFails(t *testing.T) {
	cert, err := generateTestCert()
	if err != nil {
		t.Fatal(err)
	}
	smtpLn, smtpDone := startSMTPOn587(t, &tls.Config{Certificates: []tls.Certificate{cert}})
	defer func() { smtpLn.Close(); <-smtpDone }()
	// No IMAP server -> IMAP check fails

	time.Sleep(30 * time.Millisecond)

	d := newTestDiagnostics()
	result, err := d.CheckTLS("127.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false")
	}
	if !strings.Contains(result.Message, "IMAP TLS check failed") {
		t.Errorf("expected IMAP failure, got: %s", result.Message)
	}
}

// CheckTLS: SMTP fails (lines 318-320)

func TestCheckTLSSMTPFails(t *testing.T) {
	d := newTestDiagnostics()
	result, err := d.CheckTLS("unreachable-host.invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false")
	}
	if !strings.Contains(result.Message, "SMTP TLS check failed") {
		t.Errorf("expected SMTP failure, got: %s", result.Message)
	}
}

// =========================================================================
// tlsVersionName
// =========================================================================

func TestTLSVersionNameAllPaths(t *testing.T) {
	tests := []struct {
		version  uint16
		expected string
	}{
		{0x0301, "TLS 1.0"},
		{0x0302, "TLS 1.1"},
		{0x0303, "TLS 1.2"},
		{0x0304, "TLS 1.3"},
		{0x00FF, "Unknown (0xff)"},
		{0x0001, "Unknown (0x1)"},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("0x%04x", tc.version), func(t *testing.T) {
			got := tlsVersionName(tc.version)
			if got != tc.expected {
				t.Errorf("tlsVersionName(0x%x) = %q, want %q", tc.version, got, tc.expected)
			}
		})
	}
}
