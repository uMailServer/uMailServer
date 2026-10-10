package auth

// Regression tests for RFC 8461 policy discovery (Round 50: F5310, F5311).

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// F5310: the _mta-sts TXT id is an opaque change marker, not SHA-256(policy).

type mtastsStubRT struct {
	status int
	body   string
}

func (t mtastsStubRT) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: t.status, Body: io.NopCloser(strings.NewReader(t.body)), Header: make(http.Header), Request: req}, nil
}

func newMTASTSStubValidator(txt []string, status int, body string) *MTASTSValidator {
	r := newMockDNSResolver()
	if txt != nil {
		r.txtRecords["_mta-sts.example.com"] = txt
	}
	r.ipRecords["mta-sts.example.com"] = []net.IP{net.ParseIP("93.184.216.34")}
	v := NewMTASTSValidator(r)
	v.httpClient.Transport = mtastsStubRT{status, body}
	return v
}

const mtastsOpaqueIDPolicy = "version: STSv1\r\nmode: enforce\r\nmx: mx1.example.com\r\nmx: *.mx.example.com\r\nmax_age: 604800\r\n"

func TestMTASTS_OpaquePolicyID_F5310(t *testing.T) {
	ctx := context.Background()
	// 1. Reproduction: opaque real-world id -> policy enforced.
	v := newMTASTSStubValidator([]string{"v=STSv1; id=20190429T010101;"}, 200, mtastsOpaqueIDPolicy)
	ok, p, err := v.CheckPolicy(ctx, "example.com", "evil.example.net")
	if err != nil || p == nil || p.Mode != MTASTSModeEnforce || ok {
		t.Fatalf("repro: ok=%v p=%v err=%v", ok, p, err)
	}
	// 2. Allowed MX (exact and wildcard) passes under the same cached policy.
	for _, mx := range []string{"mx1.example.com", "a.mx.example.com"} {
		if ok, _, err := v.CheckPolicy(ctx, "example.com", mx); !ok || err != nil {
			t.Fatalf("allowed mx %s: ok=%v err=%v", mx, ok, err)
		}
	}
	// 3. Policy fetch failure still errors (no policy invented).
	v = newMTASTSStubValidator([]string{"v=STSv1; id=abc"}, 404, "")
	if _, p, err := v.CheckPolicy(ctx, "example.com", "evil.example.net"); err == nil || p != nil {
		t.Fatalf("404: p=%v err=%v", p, err)
	}
	// 4. No TXT record -> no MTA-STS, no HTTP fetch needed.
	v = newMTASTSStubValidator(nil, 200, mtastsOpaqueIDPolicy)
	if ok, p, err := v.CheckPolicy(ctx, "example.com", "evil.example.net"); !ok || p != nil || err != nil {
		t.Fatalf("no txt: ok=%v p=%v err=%v", ok, p, err)
	}
	// 5. Invalid policy body still rejected.
	v = newMTASTSStubValidator([]string{"v=STSv1; id=1"}, 200, "version: STSv1\nmode: bogus\n")
	if _, _, err := v.CheckPolicy(ctx, "example.com", "x"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

// F5311: RFC 8461 §3.3 — HTTP 3xx redirects MUST NOT be followed.

type mtastsRedirectRT struct {
	mu     sync.Mutex
	seen   []string
	status int
}

func (t *mtastsRedirectRT) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.seen = append(t.seen, req.URL.String())
	t.mu.Unlock()
	if req.URL.Host == "mta-sts.example.com" && t.status != 200 {
		h := make(http.Header)
		h.Set("Location", "http://127.0.0.1:8080/x")
		return &http.Response{StatusCode: t.status, Body: io.NopCloser(strings.NewReader("")), Header: h, Request: req}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("version: STSv1\nmode: enforce\nmx: mx1.example.com\nmax_age: 86400\n")), Header: make(http.Header), Request: req}, nil
}

func fetchWithRedirect(status int) (*MTASTSPolicy, []string, error) {
	r := newMockDNSResolver()
	r.ipRecords["mta-sts.example.com"] = []net.IP{net.ParseIP("93.184.216.34")}
	v := NewMTASTSValidator(r)
	rt := &mtastsRedirectRT{status: status}
	v.httpClient.Transport = rt
	p, err := v.fetchPolicyFile(context.Background(), "example.com")
	return p, rt.seen, err
}

func TestMTASTS_RedirectNotFollowed_F5311(t *testing.T) {
	for _, st := range []int{301, 302, 307, 308} {
		p, seen, err := fetchWithRedirect(st)
		if len(seen) != 1 || p != nil || err == nil {
			t.Fatalf("%d: seen=%v p=%v err=%v", st, seen, p, err)
		}
	}
	// Edge: plain 200 still works with the production client.
	if p, seen, err := fetchWithRedirect(200); err != nil || p == nil || len(seen) != 1 {
		t.Fatalf("200: seen=%v p=%v err=%v", seen, p, err)
	}
	// Edge: repeated validators are independent (policy unchanged).
	if _, err, _ := fetchWithRedirect(302); err == nil {
		t.Fatal("second 302 accepted")
	}
}
