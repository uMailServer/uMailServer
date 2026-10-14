package smtp

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

// r292Start serves a submission server on 127.0.0.1:0, over implicit TLS when
// implicit is true. Accepted credentials are user/pass.
func r292Start(t *testing.T, implicit bool) string {
	t.Helper()
	cert, err := generateTestCert()
	if err != nil {
		t.Fatal(err)
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	srv := NewServer(&Config{Hostname: "mx.test", IsSubmission: true, TLSConfig: tlsCfg}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetAuthHandler(func(u, p string) (bool, error) { return u == "user" && p == "pass", nil })
	var ln net.Listener
	if implicit {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Stop() })
	return ln.Addr().String()
}

type r292Conn struct {
	t *testing.T
	c net.Conn
	r *bufio.Reader
}

func r292Dial(t *testing.T, addr string, implicit bool) *r292Conn {
	t.Helper()
	var c net.Conn
	var err error
	if implicit {
		c, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	} else {
		c, err = net.Dial("tcp", addr)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	rc := &r292Conn{t: t, c: c, r: bufio.NewReader(c)}
	rc.reply() // greeting
	return rc
}

// reply reads one (possibly multi-line) reply and returns its lines.
func (rc *r292Conn) reply() []string {
	rc.t.Helper()
	var lines []string
	for {
		l, err := rc.r.ReadString('\n')
		if err != nil {
			rc.t.Fatalf("read: %v", err)
		}
		lines = append(lines, strings.TrimSpace(l))
		if len(l) >= 4 && l[3] == ' ' {
			return lines
		}
	}
}

func (rc *r292Conn) cmd(line string) []string {
	rc.t.Helper()
	if _, err := io.WriteString(rc.c, line+"\r\n"); err != nil {
		rc.t.Fatal(err)
	}
	return rc.reply()
}

func r292Plain(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte("\x00" + user + "\x00" + pass))
}

// F7660: on an implicit-TLS listener (submission port 465) the session is
// already encrypted: EHLO offers AUTH and not STARTTLS, and AUTH works.
func TestImplicitTLSAdvertisesAuthNotStartTLS(t *testing.T) {
	rc := r292Dial(t, r292Start(t, true), true)
	ehlo := strings.Join(rc.cmd("EHLO client.test"), "\n")
	if !strings.Contains(ehlo, "AUTH ") {
		t.Fatalf("AUTH not advertised over implicit TLS:\n%s", ehlo)
	}
	if strings.Contains(ehlo, "STARTTLS") {
		t.Fatalf("STARTTLS advertised on an already-encrypted connection:\n%s", ehlo)
	}
}

func TestImplicitTLSAuthentication(t *testing.T) {
	addr := r292Start(t, true)
	rc := r292Dial(t, addr, true)
	rc.cmd("EHLO client.test")
	if got := rc.cmd("AUTH PLAIN " + r292Plain("user", "pass")); !strings.HasPrefix(got[0], "235") {
		t.Fatalf("good credentials: %v", got)
	}
	bad := r292Dial(t, addr, true)
	bad.cmd("EHLO client.test")
	if got := bad.cmd("AUTH PLAIN " + r292Plain("user", "wrong")); !strings.HasPrefix(got[0], "535") {
		t.Fatalf("bad credentials: %v", got)
	}
}

func TestImplicitTLSRefusesStartTLS(t *testing.T) {
	rc := r292Dial(t, r292Start(t, true), true)
	rc.cmd("EHLO client.test")
	if got := rc.cmd("STARTTLS"); !strings.HasPrefix(got[0], "503") {
		t.Fatalf("STARTTLS on a TLS connection: %v", got)
	}
}

// Control: a plain listener still offers STARTTLS first and refuses AUTH until
// the connection is encrypted.
func TestPlainListenerStillRequiresStartTLS(t *testing.T) {
	rc := r292Dial(t, r292Start(t, false), false)
	ehlo := strings.Join(rc.cmd("EHLO client.test"), "\n")
	if !strings.Contains(ehlo, "STARTTLS") || strings.Contains(ehlo, "AUTH ") {
		t.Fatalf("plain listener EHLO:\n%s", ehlo)
	}
	if got := rc.cmd("AUTH PLAIN " + r292Plain("user", "pass")); strings.HasPrefix(got[0], "235") {
		t.Fatalf("AUTH accepted before TLS: %v", got)
	}
}
