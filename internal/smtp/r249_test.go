package smtp

import (
	"bufio"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
)

// r249Run feeds commands to a fresh session and returns each reply's first
// line. Optional setup runs before the commands.
func r249Run(t *testing.T, cfg *Config, setup func(*Server, *Session), cmds ...string) []string {
	t.Helper()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if cfg == nil {
		cfg = &Config{MaxMessageSize: 1 << 20, MaxRecipients: 10, Hostname: "mx.local.test"}
	}
	srv := &Server{config: cfg}
	s := NewSession(a, srv)
	s.state = StateGreeted
	if setup != nil {
		setup(srv, s)
	}
	rd := bufio.NewReader(b)
	var out []string
	for _, c := range cmds {
		errc := make(chan error, 1)
		go func() { errc <- s.HandleCommand(c) }()
		var first string
		for {
			l, err := rd.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if first == "" {
				first = strings.TrimSpace(l)
			}
			if len(l) < 4 || l[3] != '-' {
				break
			}
		}
		<-errc
		out = append(out, first)
	}
	return out
}

func r249Code(t *testing.T, want string, got string) {
	t.Helper()
	if !strings.HasPrefix(got, want) {
		t.Fatalf("want reply %s*, got %q", want, got)
	}
}

// F7260: RFC 5321 §4.5.3.1: local part <= 64, domain <= 255, path <= 256.
func TestF7260_AddressLengthLimits(t *testing.T) {
	long := strings.Repeat("a", 65)
	dom := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 63) + ".com"
	for _, addr := range []string{long + "@b.com", "a@" + dom + ".example.org"} {
		r := r249Run(t, nil, nil, "MAIL FROM:<"+addr+">")
		r249Code(t, "501", r[0])
		r = r249Run(t, nil, nil, "MAIL FROM:<x@y.com>", "RCPT TO:<"+addr+">")
		r249Code(t, "501", r[1])
	}
	ok := strings.Repeat("a", 64) + "@b.com"
	r := r249Run(t, nil, nil, "MAIL FROM:<"+ok+">")
	r249Code(t, "250", r[0])
}

// F7261: unbalanced / doubled angle brackets are not a valid path.
func TestF7261_BracketBalance(t *testing.T) {
	for _, p := range []string{"<a@b.com", "<<a@b.com>>", "a@b.com>"} {
		r := r249Run(t, nil, nil, "MAIL FROM:"+p)
		r249Code(t, "501", r[0])
		r = r249Run(t, nil, nil, "MAIL FROM:<x@y.com>", "RCPT TO:"+p)
		r249Code(t, "501", r[1])
	}
	r := r249Run(t, nil, nil, "MAIL FROM:<>", "RCPT TO:<a@b.com>")
	r249Code(t, "250", r[0])
	r249Code(t, "250", r[1])
}

// F7262: ENVID/ORCPT/AUTH carry xtext (RFC 3461 §4); ENVID is at most 100 chars.
func TestF7262_XtextParams(t *testing.T) {
	bad := []string{"ENVID=a=b", "ENVID=a+zz", "ENVID=" + strings.Repeat("x", 101), "ENVID=caf\xc3\xa9", "AUTH=a=b"}
	for _, p := range bad {
		r := r249Run(t, nil, nil, "MAIL FROM:<a@b.com> "+p)
		r249Code(t, "501", r[0])
	}
	for _, p := range []string{"ENVID=a+2Bz", "ENVID=" + strings.Repeat("x", 100), "AUTH=<>", "AUTH=a+3Db"} {
		r := r249Run(t, nil, nil, "MAIL FROM:<a@b.com> "+p)
		r249Code(t, "250", r[0])
	}
	for _, p := range []string{"ORCPT=rfc822;a=b", "ORCPT=rfc822;a+zz"} {
		r := r249Run(t, nil, nil, "MAIL FROM:<x@y.com>", "RCPT TO:<a@b.com> "+p)
		r249Code(t, "501", r[1])
	}
	r := r249Run(t, nil, nil, "MAIL FROM:<x@y.com>", "RCPT TO:<a@b.com> ORCPT=rfc822;a+3Db@c.com")
	r249Code(t, "250", r[1])
}

// F7263: RFC 5321 §4.5.1: RCPT TO:<Postmaster> (no domain) MUST be accepted.
func TestF7263_PostmasterNoDomain(t *testing.T) {
	var got string
	r := r249Run(t, nil, func(srv *Server, s *Session) {
		srv.onValidate = func(from string, to []string) error { got = to[0]; return nil }
	}, "MAIL FROM:<x@y.com>", "RCPT TO:<Postmaster>")
	r249Code(t, "250", r[1])
	if got != "postmaster@mx.local.test" {
		t.Fatalf("recipient = %q", got)
	}
}

// F7264: a submission server never accepts unauthenticated mail even when
// RequireAuth was not set explicitly.
func TestF7264_SubmissionRequiresAuth(t *testing.T) {
	cfg := &Config{MaxMessageSize: 1 << 20, MaxRecipients: 10, Hostname: "h", IsSubmission: true}
	r := r249Run(t, cfg, nil, "MAIL FROM:<a@b.com>")
	r249Code(t, "530", r[0])
}

// F7265: an authenticated submission's From header must belong to the user
// (RFC 6409 §6.2 / §8); otherwise the DATA is refused with 553 5.7.1.
func TestF7265_FromHeaderOwnership(t *testing.T) {
	run := func(from string) (string, bool) {
		a, b := net.Pipe()
		go io.Copy(io.Discard, b)
		defer a.Close()
		defer b.Close()
		delivered := false
		srv := &Server{config: &Config{MaxMessageSize: 1 << 20, MaxRecipients: 10, Hostname: "h"},
			onDeliver:       func(string, []string, []byte) error { delivered = true; return nil },
			onSenderAllowed: func(u, f string) bool { return f == "alice@x.test" }}
		s := NewSession(a, srv)
		s.state = StateGreeted
		s.isAuth, s.username = true, "alice@x.test"
		s.HandleCommand("MAIL FROM:<alice@x.test>")
		s.HandleCommand("RCPT TO:<bob@y.test>")
		s.reader = bufio.NewReader(strings.NewReader("From: " + from + "\r\nTo: bob@y.test\r\n\r\nhi\r\n.\r\n"))
		s.HandleCommand("DATA")
		return "", delivered
	}
	if _, d := run("Alice <alice@x.test>"); !d {
		t.Fatal("own From refused")
	}
	if _, d := run("CEO <ceo@x.test>"); d {
		t.Fatal("forged From header delivered")
	}
	if _, d := run("Alice <alice@x.test>, ceo@x.test"); d {
		t.Fatal("forged second From address delivered")
	}
}

func TestF7265_FromHeaderOwnershipBDAT(t *testing.T) {
	a, b := net.Pipe()
	go io.Copy(io.Discard, b)
	defer a.Close()
	defer b.Close()
	delivered := false
	srv := &Server{config: &Config{MaxMessageSize: 1 << 20, MaxRecipients: 10, Hostname: "h"},
		onDeliver:       func(string, []string, []byte) error { delivered = true; return nil },
		onSenderAllowed: func(u, f string) bool { return f == "alice@x.test" }}
	s := NewSession(a, srv)
	s.state = StateGreeted
	s.isAuth, s.username = true, "alice@x.test"
	s.HandleCommand("MAIL FROM:<alice@x.test>")
	s.HandleCommand("RCPT TO:<bob@y.test>")
	msg := "From: ceo@x.test\r\n\r\nhi\r\n"
	s.reader = bufio.NewReader(strings.NewReader(msg))
	s.HandleCommand("BDAT " + strconv.Itoa(len(msg)) + " LAST")
	if delivered {
		t.Fatal("forged From header delivered via BDAT")
	}
}
