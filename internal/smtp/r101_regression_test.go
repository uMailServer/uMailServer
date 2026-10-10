package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

type rblStubResolver struct {
	ip  net.IP
	err error
	got string
}

func (r *rblStubResolver) LookupHost(ctx context.Context, host string) (net.IP, error) {
	r.got = host
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("no deadline")
	}
	return r.ip, r.err
}

// F5831: STARTTLS must answer 454 and not be advertised when the TLS config
// cannot supply a certificate.
func TestR101_STARTTLSNoCertificate(t *testing.T) {
	srv := &Server{config: &Config{TLSConfig: &tls.Config{}}}
	if srv.tlsAvailable() {
		t.Fatal("empty tls.Config must not count as TLS available")
	}
	srv.config.TLSConfig = nil
	if srv.tlsAvailable() {
		t.Fatal("nil tls.Config must not count as TLS available")
	}
	srv.config.TLSConfig = &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return nil, nil }}
	if !srv.tlsAvailable() {
		t.Fatal("GetCertificate should count as available")
	}
}

// F5830: AV scan error temp-fails unless fail-open is configured.
func TestR101_AVScanErrorTempFails(t *testing.T) {
	sc := &mockAVScanner{enabled: true, scanErr: errors.New("down")}
	stage := NewAVStage(sc, "reject")
	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "a@b.c", []string{"r@b.c"}, []byte("x"))
	if r := stage.Process(ctx); r != ResultReject || ctx.RejectionCode != 451 {
		t.Fatalf("got %v/%d", r, ctx.RejectionCode)
	}
	stage.SetFailOpen(true)
	ctx = NewMessageContext(net.ParseIP("1.2.3.4"), "a@b.c", []string{"r@b.c"}, []byte("x"))
	if r := stage.Process(ctx); r != ResultAccept {
		t.Fatalf("fail-open should accept, got %v", r)
	}
	// nil result without error must not panic
	stage = NewAVStage(&mockAVScanner{enabled: true}, "reject")
	ctx = NewMessageContext(net.ParseIP("1.2.3.4"), "a@b.c", []string{"r@b.c"}, []byte("x"))
	if r := stage.Process(ctx); r != ResultReject {
		t.Fatalf("nil result: %v", r)
	}
}

func TestR101_AVSkippedAcceptedNotClean(t *testing.T) {
	stage := NewAVStage(&mockAVScanner{enabled: true, result: &AVScanResult{Skipped: true}}, "reject")
	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "a@b.c", []string{"r@b.c"}, []byte("x"))
	if r := stage.Process(ctx); r != ResultAccept {
		t.Fatalf("got %v", r)
	}
	if ctx.Headers["X-Virus-Scan"][0] != "skipped" {
		t.Fatal("skipped scan must be marked")
	}
}

// F5833: IPv6 nibbles must be hex.
func TestR101_ReverseIPv6Hex(t *testing.T) {
	got := reverseIP("2001:db8::abcd")
	if !strings.HasPrefix(got, "d.c.b.a.") || !strings.HasSuffix(got, "8.b.d.0.1.0.0.2.ip6.arpa") {
		t.Fatalf("got %q", got)
	}
}

// F5835: only 127/8 (excluding 127.255.x operator errors) are listings.
func TestR101_RBLResponseCodes(t *testing.T) {
	cases := map[string]float64{"127.0.0.2": 3.0, "127.255.255.254": 0, "8.8.8.8": 0, "::1": 0}
	for ip, want := range cases {
		st := NewRBLStage([]string{"bl.test"}, &rblStubResolver{ip: net.ParseIP(ip)})
		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "a@b.c", []string{"r@b.c"}, nil)
		st.Process(ctx)
		if ctx.SpamScore != want {
			t.Errorf("%s: score %v want %v", ip, ctx.SpamScore, want)
		}
	}
}

// F5832: greylist normalizes case, registers all recipients, expires entries.
func TestR101_GreylistCaseAndMultiRecipient(t *testing.T) {
	g := NewGreylistStage()
	ip := net.ParseIP("1.2.3.4")
	ctx := NewMessageContext(ip, "A@B.c", []string{"r1@x.y", "R2@x.y"}, nil)
	if g.Process(ctx) != ResultReject {
		t.Fatal("first should defer")
	}
	if len(g.greylist) != 2 {
		t.Fatalf("all recipients should be registered, got %d", len(g.greylist))
	}
	for _, e := range g.greylist {
		e.firstSeen = time.Now().Add(-6 * time.Minute)
	}
	ctx = NewMessageContext(ip, "a@b.c", []string{"R1@x.y", "r2@x.y"}, nil)
	if g.Process(ctx) != ResultAccept {
		t.Fatal("case-insensitive retry should pass")
	}
	// stale pending entry restarts the greylist window
	for _, e := range g.greylist {
		e.allowed = false
		e.firstSeen = time.Now().Add(-7 * time.Hour)
	}
	ctx = NewMessageContext(ip, "a@b.c", []string{"r1@x.y"}, nil)
	if g.Process(ctx) != ResultReject {
		t.Fatal("expired pending entry must be re-greylisted")
	}
}
