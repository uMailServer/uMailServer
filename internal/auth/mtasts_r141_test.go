package auth

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type r141Resolver struct {
	mu  sync.Mutex
	txt string
	err error
}

func (r *r141Resolver) set(txt string, err error) {
	r.mu.Lock()
	r.txt, r.err = txt, err
	r.mu.Unlock()
}
func (r *r141Resolver) LookupTXT(context.Context, string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	if r.txt == "" {
		return nil, &net.DNSError{IsNotFound: true}
	}
	return []string{r.txt}, nil
}
func (r *r141Resolver) LookupIP(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}
func (r *r141Resolver) LookupMX(context.Context, string) ([]*net.MX, error) { return nil, nil }

type r141Env struct {
	v      *MTASTSValidator
	dns    *r141Resolver
	mu     sync.Mutex
	body   string
	status int
	hits   int
}

func newR141Env(t *testing.T, body string) *r141Env {
	t.Helper()
	e := &r141Env{body: body, status: 200, dns: &r141Resolver{txt: "v=STSv1; id=1"}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.hits++
		w.WriteHeader(e.status)
		_, _ = w.Write([]byte(e.body))
	}))
	t.Cleanup(srv.Close)
	e.v = NewMTASTSValidator(e.dns)
	e.v.SetHTTPClient(&http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test server
			DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, n, srv.Listener.Addr().String())
			},
		},
	})
	e.v.recheck = time.Millisecond
	return e
}

func (e *r141Env) setServer(body string, status int) {
	e.mu.Lock()
	e.body, e.status = body, status
	e.mu.Unlock()
}

const r141Enforce = "version: STSv1\nmode: enforce\nmx: mx.example.test\nmax_age: 86400\n"

// A failed refresh must not discard a valid cached enforce policy.
func TestR141_FailedRefreshKeepsCachedEnforce(t *testing.T) {
	e := newR141Env(t, r141Enforce)
	ctx := context.Background()
	if p, err := e.v.GetPolicy(ctx, "example.test"); err != nil || p == nil || p.Mode != MTASTSModeEnforce {
		t.Fatalf("initial fetch: %v %v", p, err)
	}
	// id changes but the policy host is down: keep enforcing the old policy.
	e.dns.set("v=STSv1; id=2", nil)
	e.setServer("", 503)
	time.Sleep(5 * time.Millisecond)
	p, err := e.v.GetPolicy(ctx, "example.test")
	if err != nil || p == nil || p.Mode != MTASTSModeEnforce {
		t.Fatalf("cached enforce policy dropped on failed refresh: %v %v", p, err)
	}
	// TXT record vanished or DNS failing: still enforced until max_age.
	e.dns.set("", nil)
	time.Sleep(5 * time.Millisecond)
	if p, err := e.v.GetPolicy(ctx, "example.test"); err != nil || p == nil {
		t.Fatalf("policy dropped when TXT vanished: %v %v", p, err)
	}
	e.dns.set("", &net.DNSError{IsTemporary: true, IsTimeout: true})
	time.Sleep(5 * time.Millisecond)
	if p, err := e.v.GetPolicy(ctx, "example.test"); err != nil || p == nil {
		t.Fatalf("policy dropped on DNS failure: %v %v", p, err)
	}
}

// A changed TXT id triggers a refetch; an unchanged id does not.
func TestR141_IDChangeRefetches(t *testing.T) {
	e := newR141Env(t, r141Enforce)
	ctx := context.Background()
	if _, err := e.v.GetPolicy(ctx, "example.test"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := e.v.GetPolicy(ctx, "example.test"); err != nil {
		t.Fatal(err)
	}
	if e.hits != 1 {
		t.Fatalf("unchanged id must not refetch, hits=%d", e.hits)
	}
	e.setServer("version: STSv1\nmode: testing\nmx: mx.example.test\nmax_age: 86400\n", 200)
	e.dns.set("v=STSv1; id=2", nil)
	time.Sleep(5 * time.Millisecond)
	p, err := e.v.GetPolicy(ctx, "example.test")
	if err != nil || p == nil || p.Mode != MTASTSModeTesting {
		t.Fatalf("id change must refetch the policy: %v %v", p, err)
	}
}

// With no cache a fetch failure is reported (the caller logs and delivers
// opportunistically per RFC 8461 3.3) and is not cached as "no policy".
func TestR141_NoCacheFetchErrorReported(t *testing.T) {
	e := newR141Env(t, "")
	e.setServer("", 500)
	if p, err := e.v.GetPolicy(context.Background(), "example.test"); err == nil || p != nil {
		t.Fatalf("want fetch error, got %v %v", p, err)
	}
	e.setServer(r141Enforce, 200)
	if p, err := e.v.GetPolicy(context.Background(), "example.test"); err != nil || p == nil {
		t.Fatalf("error must not be negative-cached: %v %v", p, err)
	}
}

func TestR141_MatchMX(t *testing.T) {
	cases := []struct {
		pat, mx string
		want    bool
	}{
		{"MX.Example.Test", "mx.example.test.", true},
		{"*.example.test", "MX1.EXAMPLE.test.", true},
		{"*.example.test", "example.test", false},
		{"*.example.test", "a.b.example.test", false},
		{"mx.example.test.", "mx.example.test", true},
	}
	for _, c := range cases {
		if got := matchMX(c.pat, c.mx); got != c.want {
			t.Errorf("matchMX(%q,%q)=%v want %v", c.pat, c.mx, got, c.want)
		}
	}
	_ = strings.ToLower
}
