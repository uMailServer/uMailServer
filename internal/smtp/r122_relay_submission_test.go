package smtp

// Round 122 (F6040-F6044): relay/submission policy regressions.

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

func r122Session(t *testing.T, cfg *Config, setup func(*Server)) (*f4907Client, func()) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	if cfg.Hostname == "" {
		cfg.Hostname = "mx.test"
	}
	cfg.MaxMessageSize, cfg.MaxRecipients = 1<<20, 10
	cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 5*time.Second
	srv := NewServer(cfg, nil)
	setup(srv)
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
	c := &f4907Client{conn: clientConn, r: bufio.NewReader(clientConn)}
	c.reply(t)
	c.cmd(t, "EHLO c.test")
	return c, func() { _ = clientConn.Close(); <-done }
}

// F6040: a quoted local part containing '@' was flattened into an ambiguous
// unquoted address (a@evil.com@local.com).
func TestR122_QuotedLocalPartAtRejected(t *testing.T) {
	for _, a := range []string{`"a@evil.com"@local.com`, `"x\"y"@local.com`, `"a,b"@local.com`} {
		if got, err := ValidateEmail(a); err == nil {
			t.Errorf("ValidateEmail(%q) = %q, want error", a, got)
		}
	}
	if _, err := ValidateEmail("user@local.com"); err != nil {
		t.Fatal(err)
	}
}

// F6041: an authenticated user could send as any address.
func TestR122_SenderSpoofRejected(t *testing.T) {
	srv := NewServer(&Config{Hostname: "h", MaxMessageSize: 1 << 20, MaxRecipients: 5, RequireAuth: true, IsSubmission: true}, nil)
	srv.SetSenderAllowedHandler(func(u, from string) bool { return from == u })
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	s := NewSession(a, srv)
	s.state, s.isAuth, s.username = StateGreeted, true, "alice@x.test"
	go func() {
		r := bufio.NewReader(b)
		for {
			if _, err := r.ReadString('\n'); err != nil {
				return
			}
		}
	}()
	_ = s.handleMAIL("FROM:<bob@x.test>")
	if s.mailFrom != "" || s.state != StateGreeted {
		t.Fatalf("spoofed sender accepted: %q", s.mailFrom)
	}
	_ = s.handleMAIL("FROM:<alice@x.test>")
	if s.mailFrom != "alice@x.test" {
		t.Fatalf("own sender refused")
	}
}

// F6042: unauthenticated RCPT to a non-local domain is refused at RCPT time.
func TestR122_RelayRefusedAtRCPT(t *testing.T) {
	c, stop := r122Session(t, &Config{}, func(s *Server) {
		s.SetLocalDomainHandler(func(d string) bool { return d == "local.test" })
	})
	defer stop()
	c.cmd(t, "MAIL FROM:<x@ext.test>")
	for _, a := range []string{"u@evil.test", "u@[1.2.3.4]", "u@[IPv6:::1]", "<@r:u@evil.test>", `"a@evil.test"@local.test`, "u@local.test."} {
		if got := c.cmd(t, "RCPT TO:<"+strings.Trim(a, "<>")+">"); got != "554" && got != "501" {
			t.Errorf("RCPT %s = %s, want 554/501", a, got)
		}
	}
	for _, a := range []string{"u@local.test", "U@LOCAL.TEST"} {
		if got := c.cmd(t, "RCPT TO:<"+a+">"); got != "250" {
			t.Errorf("RCPT %s = %s, want 250", a, got)
		}
	}
}

// F6043: Bcc must be stripped and Date added on submission.
func TestR122_SanitizeSubmissionHeaders(t *testing.T) {
	in := "From: a@x\r\nBcc: secret@y,\r\n\tmore@z\r\nSubject: s\r\n\r\nBcc: body-kept\r\n"
	out := string(sanitizeSubmissionHeaders([]byte(in)))
	if strings.Contains(out, "secret@y") || strings.Contains(out, "more@z") {
		t.Fatalf("Bcc kept: %q", out)
	}
	if !strings.HasPrefix(out, "Date: ") || !strings.Contains(out, "Subject: s") || !strings.HasSuffix(out, "\r\n\r\nBcc: body-kept\r\n") {
		t.Fatalf("unexpected: %q", out)
	}
	if got := string(sanitizeSubmissionHeaders([]byte("Date: x\r\nSubject: s\r\n\r\nb"))); strings.Count(got, "Date:") != 1 {
		t.Fatalf("Date duplicated: %q", got)
	}
}

// F6044: per-user hourly recipient cap.
func TestR122_UserRecipientLimit(t *testing.T) {
	srv := NewServer(&Config{}, nil)
	srv.SetUserRecipientLimit(2)
	if !srv.allowUserRecipient("A") || !srv.allowUserRecipient("a") || srv.allowUserRecipient("a") {
		t.Fatal("cap not enforced")
	}
	if !srv.allowUserRecipient("b") {
		t.Fatal("cap not per user")
	}
}
