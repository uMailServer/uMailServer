package cli

// Regression test for the checkMX trailing-dot defect: net.LookupMX returns
// Host values as fully-qualified names ending in a dot ("mail.example.com."),
// and checkMX compared that value against the configured hostname with
// strings.EqualFold without normalization — so a fully compliant MX record
// was misreported as "warning: MX record points to different host". The
// sibling checkPTR already trims the trailing dot; checkMX now does too.
//
// The test swaps net.DefaultResolver for a resolver whose Dial reaches a
// local fake UDP DNS server answering the MX query with a canned, trailing-
// dot record — the production code path (net.LookupMX) runs unmodified, no
// external network is touched, and the fake is restored in cleanup.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

// encodeName encodes a DNS domain name into wire format (labels + NUL).
func encodeName(name string) []byte {
	var out []byte
	if len(name) > 0 && name[len(name)-1] == '.' {
		name = name[:len(name)-1]
	}
	for _, label := range splitDNSLabels(name) {
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	return append(out, 0)
}

func splitDNSLabels(name string) []string {
	var labels []string
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			if i > start {
				labels = append(labels, name[start:i])
			}
			start = i + 1
		}
	}
	return labels
}

// startFakeMXServer serves canned MX answers (exchange, preference) for any
// query arriving on a loopback UDP socket.
func startFakeMXServer(t *testing.T, exchange string, pref uint16) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake dns listen: %v", err)
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}
			query := make([]byte, n)
			copy(query, buf[:n])

			// Parse the question section: skip 4 header bytes, then labels
			// until the NUL, then qtype+qclass.
			i := 12
			for i < n && query[i] != 0 {
				i += int(query[i]) + 1
			}
			i += 5 // NUL + qtype(2) + qclass(2)
			if i > n {
				continue
			}

			resp := make([]byte, 0, 128)
			var header [12]byte
			binary.BigEndian.PutUint16(header[0:2], binary.BigEndian.Uint16(query[0:2])) // id
			binary.BigEndian.PutUint16(header[2:4], 0x8180)                              // response, recursion
			binary.BigEndian.PutUint16(header[4:6], 1)                                   // qdcount
			binary.BigEndian.PutUint16(header[6:8], 1)                                   // ancount
			resp = append(resp, header[:]...)
			resp = append(resp, query[12:i]...) // question echo

			// Answer: name ptr to first question, MX(15), IN(1), ttl(4),
			// rdlength(2), preference(2), exchange.
			rd := append([]byte{byte(pref >> 8), byte(pref)}, encodeName(exchange)...)
			answer := []byte{0xC0, 0x0C, 0, 15, 0, 1, 0, 0, 1, 43} // name ptr, MX, IN, ttl 299
			answer = append(answer, byte(len(rd)>>8), byte(len(rd)))
			answer = append(answer, rd...)
			resp = append(resp, answer...)

			_, _ = conn.WriteTo(resp, addr)
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return conn.LocalAddr().String()
}

// withFakeMXResolver swaps net.DefaultResolver for one that talks to the fake
// server, restoring the original in cleanup.
func withFakeMXResolver(t *testing.T, fakeAddr string) {
	t.Helper()
	orig := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			_ = ctx
			var d net.Dialer
			return d.DialContext(ctx, "udp", fakeAddr)
		},
	}
	t.Cleanup(func() { net.DefaultResolver = orig })
}

func TestCheckMXAcceptsTrailingDotFQDN(t *testing.T) {
	fake := startFakeMXServer(t, "mail.example.com.", 10)
	withFakeMXResolver(t, fake)

	d := NewDiagnostics(&config.Config{})
	d.config.Server.Hostname = "mail.example.com"

	results, err := d.checkMX("example.com")
	if err != nil {
		t.Fatalf("checkMX: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 MX result, got %d: %+v", len(results), results)
	}

	// Control: net.LookupMX must actually deliver the trailing-dot FQDN —
	// if the fake or the resolver changed, this guards the harness.
	if results[0].Found == "" {
		t.Fatalf("CONTROL FAILED (harness): no Found host recorded: %+v", results[0])
	}

	// The defect: a compliant MX record pointing at this very server must
	// PASS, not warn.
	if results[0].Status != "pass" {
		t.Fatalf("FAIL: compliant MX record reported as %q (%s) — want \"pass\" (Found=%q Expected=%q)",
			results[0].Status, results[0].Message, results[0].Found, results[0].Expected)
	}
}

func TestCheckMXStillWarnsForForeignHost(t *testing.T) {
	fake := startFakeMXServer(t, "other-mail.example.com.", 10)
	withFakeMXResolver(t, fake)

	d := NewDiagnostics(&config.Config{})
	d.config.Server.Hostname = "mail.example.com"

	results, err := d.checkMX("example.com")
	if err != nil {
		t.Fatalf("checkMX: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 MX result, got %d", len(results))
	}
	if results[0].Status != "warning" {
		t.Fatalf("CONTROL FAILED (harness): foreign MX should warn, got %q (%s)",
			results[0].Status, results[0].Message)
	}
}

// Guard: without the resolver swap this test would touch real DNS; ensure the
// swap mechanism itself is active by asserting the fake answers a raw lookup.
func TestFakeMXServerAnswersLookupMX(t *testing.T) {
	fake := startFakeMXServer(t, "mail.example.com.", 10)
	withFakeMXResolver(t, fake)

	deadline := time.Now().Add(2 * time.Second)
	for {
		records, err := net.LookupMX("example.com")
		if err == nil && len(records) == 1 && records[0].Host == "mail.example.com." {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("CONTROL FAILED (harness): LookupMX through the fake never returned the canned record (err=%v)", errors.Unwrap(fmt.Errorf("%v", err)))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
