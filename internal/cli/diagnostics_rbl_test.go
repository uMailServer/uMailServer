package cli

// Regression test for the RBL lookup-failure misclassification:
// checkRBLServer treated ANY net.LookupIP error (timeout, SERVFAIL, network
// outage) as "not listed", and checkRBL mapped that to Score "clean" — an
// RBL check that could not run reported the server's IP as clean, a false
// negative in the exact diagnostic whose purpose is honest reporting.
// Correct RBL semantics: NXDOMAIN means genuinely not listed; any other
// lookup failure is inconclusive and must not be reported as clean.
//
// Hermetic: net.DefaultResolver is swapped for a resolver dialing a local
// fake UDP DNS server (same pattern as diagnostics_mx_test.go) that answers
// each RBL lookup with a canned outcome — NXDOMAIN for bl.spamcop.net, a
// 127.0.0.2 listing for b.barracudacentral.org, and SERVFAIL for
// dnsbl.sorbs.net. The production path (checkRBL -> net.LookupIP) runs
// unmodified; no external network is touched.

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

// fakeRBLBehavior maps a queried name (lowercased) to a canned outcome.
type fakeRBLBehavior func(qname string, qtype uint16) (rcode int, answers [][]byte)

func startFakeRBLServer(t *testing.T, behavior fakeRBLBehavior) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake rbl dns listen: %v", err)
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
			var labels []string
			for i < n && query[i] != 0 {
				l := int(query[i])
				if i+1+l > n {
					break
				}
				labels = append(labels, strings.ToLower(string(query[i+1:i+1+l])))
				i += l + 1
			}
			// i points at the root NUL; the question section spans through
			// NUL + qtype(2) + qclass(2).
			if i+5 > n {
				continue
			}
			qtype := binary.BigEndian.Uint16(query[i+1 : i+3])
			qname := strings.Join(labels, ".")

			rcode, answers := behavior(qname, qtype)

			resp := make([]byte, 0, 128)
			var header [12]byte
			binary.BigEndian.PutUint16(header[0:2], binary.BigEndian.Uint16(query[0:2])) // id
			flags := uint16(0x8000 | 0x0100 | rcode)                                     // response, recursion, rcode
			binary.BigEndian.PutUint16(header[2:4], flags)
			binary.BigEndian.PutUint16(header[4:6], 1) // qdcount
			binary.BigEndian.PutUint16(header[6:8], uint16(len(answers)))
			resp = append(resp, header[:]...)
			resp = append(resp, query[12:i+5]...) // question echo
			for _, ans := range answers {
				// name ptr to first question, type, IN, ttl, rdlength, rdata
				rec := []byte{0xC0, 0x0C}
				rec = binary.BigEndian.AppendUint16(rec, qtype)
				rec = binary.BigEndian.AppendUint16(rec, 1)
				rec = binary.BigEndian.AppendUint32(rec, 299)
				rec = binary.BigEndian.AppendUint16(rec, uint16(len(ans)))
				rec = append(rec, ans...)
				resp = append(resp, rec...)
			}

			_, _ = conn.WriteTo(resp, addr)
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return conn.LocalAddr().String()
}

func withFakeRBLResolver(t *testing.T, fakeAddr string) {
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

const (
	rblRCODENoError   = 0
	rblRCODEServFail  = 2
	rblRCODENXDomain  = 3
	rblTypeA          = 1
	rblTypeAAAA       = 28
	rblAnswerA127_0_0 = "\x7f\x00\x00\x02"
	rblAnswerALOCAL   = "\x7f\x00\x00\x01"
)

func TestCheckRBLReportsLookupFailureAsInconclusive(t *testing.T) {
	fake := startFakeRBLServer(t, func(qname string, qtype uint16) (int, [][]byte) {
		switch {
		case qname == "localhost":
			// checkRBL resolves the configured hostname first.
			if qtype == rblTypeA {
				return rblRCODENoError, [][]byte{[]byte(rblAnswerALOCAL)}
			}
			return rblRCODENoError, nil
		case strings.HasSuffix(qname, ".bl.spamcop.net"):
			// NXDOMAIN: genuinely not listed.
			return rblRCODENXDomain, nil
		case strings.HasSuffix(qname, ".b.barracudacentral.org"):
			// Listed (127.0.0.2 response code).
			if qtype == rblTypeA {
				return rblRCODENoError, [][]byte{[]byte(rblAnswerA127_0_0)}
			}
			return rblRCODENoError, nil
		case strings.HasSuffix(qname, ".dnsbl.sorbs.net"):
			// Infrastructure failure: the lookup could not run.
			return rblRCODEServFail, nil
		default:
			return rblRCODENXDomain, nil
		}
	})
	withFakeRBLResolver(t, fake)

	d := NewDiagnostics(&config.Config{Server: config.ServerConfig{Hostname: "localhost"}})

	results, issues := d.checkRBL()
	if len(results) != 3 {
		t.Fatalf("FAIL: %d RBL results, want 3", len(results))
	}

	byServer := map[string]RBLCheckResult{}
	for _, r := range results {
		byServer[r.Server] = r
	}

	// Control: NXDOMAIN is genuinely not listed.
	if r := byServer["bl.spamcop.net"]; r.Score != "clean" || r.Listed {
		t.Errorf("FAIL: NXDOMAIN RBL reported Score=%q Listed=%v, want clean/not listed", r.Score, r.Listed)
	}
	// Control: a 127.0.0.2 answer is a listing.
	if r := byServer["b.barracudacentral.org"]; !r.Listed || r.Score != "spam" {
		t.Errorf("FAIL: listed RBL reported Score=%q Listed=%v, want spam/listed", r.Score, r.Listed)
	}
	// DEFECT: a SERVFAIL (lookup failure) must be inconclusive — not clean.
	if r := byServer["dnsbl.sorbs.net"]; r.Score != "inconclusive" {
		t.Errorf("FAIL: SERVFAIL RBL reported Score=%q (Message=%q), want \"inconclusive\" — a failed lookup is not evidence the IP is clean", r.Score, r.Message)
	}
	foundIssue := false
	for _, issue := range issues {
		if strings.Contains(issue, "dnsbl.sorbs.net") && strings.Contains(issue, "lookup failed") {
			foundIssue = true
		}
	}
	if !foundIssue {
		t.Errorf("FAIL: issues %q do not surface the dnsbl.sorbs.net lookup failure", issues)
	}
}
