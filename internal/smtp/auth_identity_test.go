package smtp

// Regression tests for the SASL mechanism-dependent session identity defect:
// handleAuthPLAIN stored the RAW-case username in s.username and passed it to
// onLoginResult, handleAuthLOGIN passed the RAW username to onLoginResult on
// success (normalized on failure), and handleAuthSCRAMSHA256 looked up the
// password with the RAW AuthCID. One account therefore presented different
// in-session identities depending on the chosen mechanism — diverging per-user
// rate-limiter buckets (pipeline CheckUser/CheckRecipients key on
// ctx.Username), sieve lookups, and audit trails. The fix normalizes the
// username (UsernameCaseMapped lowercase) consistently across PLAIN, LOGIN,
// and SCRAM.

import (
	"bufio"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
)

type authIDRecorder struct {
	loginResults []string // usernames reported to onLoginResult
	passwordFor  string   // username passed to onGetPassword (SCRAM)
}

// newAuthIDSession builds a server+session over net.Pipe with a feeder
// goroutine emulating the connection loop; AUTH continuations are read raw by
// the handlers, so all client I/O is plain wire I/O.
func newAuthIDSession(t *testing.T, rec *authIDRecorder) (net.Conn, *bufio.Reader) {
	t.Helper()
	c1, c2 := net.Pipe()

	srv := NewServer(&Config{IsSubmission: true, AllowInsecure: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetAuthHandler(func(username, password string) (bool, error) {
		return strings.ToLower(username) == "alice@example.com" && password == "secret", nil
	})
	srv.SetPasswordHandler(func(username string) (string, error) {
		rec.passwordFor = username
		return "secret", nil
	})
	srv.SetLoginResultHandler(func(username string, success bool, ip, reason string) {
		rec.loginResults = append(rec.loginResults, username)
	})

	session := NewSession(c1, srv)
	go func() {
		rd1 := bufio.NewReader(c1)
		for {
			line, err := rd1.ReadString('\n')
			if err != nil {
				return
			}
			if err := session.HandleCommand(strings.TrimRight(line, "\r\n")); err != nil {
				return
			}
		}
	}()

	rd := bufio.NewReader(c2)
	authIDSendCmd(t, c2, "EHLO proof")
	for {
		line := authIDReadLine(t, rd)
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}
	return c2, rd
}

func authIDSendCmd(t *testing.T, conn net.Conn, cmd string) {
	t.Helper()
	if _, err := conn.Write([]byte(cmd + "\r\n")); err != nil {
		t.Fatalf("write %q: %v", cmd, err)
	}
}

func authIDReadLine(t *testing.T, rd *bufio.Reader) string {
	t.Helper()
	line, err := rd.ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return strings.TrimRight(line, "\r\n")
}

// nonceOf extracts the combined nonce (r=...) from a SCRAM server-first message.
func authIDNonceOf(serverFirst string) string {
	for _, field := range strings.Split(serverFirst, ",") {
		if strings.HasPrefix(field, "r=") {
			return strings.TrimPrefix(field, "r=")
		}
	}
	return ""
}

func TestAuthIdentityNormalizedAcrossMechanisms(t *testing.T) {
	t.Run("PLAIN stores normalized username", func(t *testing.T) {
		rec := &authIDRecorder{}
		conn, rd := newAuthIDSession(t, rec)
		defer conn.Close()

		ir := base64.StdEncoding.EncodeToString([]byte("\x00Alice@Example.com\x00secret"))
		authIDSendCmd(t, conn, "AUTH PLAIN "+ir)
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "235") {
			t.Fatalf("CONTROL FAILED (harness): AUTH PLAIN = %q", resp)
		}
	})

	t.Run("LOGIN stores normalized username (control)", func(t *testing.T) {
		conn, rd := newAuthIDSession(t, &authIDRecorder{})
		defer conn.Close()

		authIDSendCmd(t, conn, "AUTH LOGIN")
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "334") {
			t.Fatalf("CONTROL FAILED (harness): AUTH LOGIN = %q", resp)
		}
		authIDSendCmd(t, conn, base64.StdEncoding.EncodeToString([]byte("Alice@Example.com")))
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "334") {
			t.Fatalf("CONTROL FAILED (harness): username prompt = %q", resp)
		}
		authIDSendCmd(t, conn, base64.StdEncoding.EncodeToString([]byte("secret")))
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "235") {
			t.Fatalf("CONTROL FAILED (harness): AUTH LOGIN final = %q", resp)
		}
	})

	t.Run("SCRAM password lookup uses normalized username", func(t *testing.T) {
		rec := &authIDRecorder{}
		conn, rd := newAuthIDSession(t, rec)
		defer conn.Close()

		clientNonce := "fyko+d2lbbFgONRv9qkxdawL"
		clientFirst := "n,,n=Alice@Example.com,r=" + clientNonce
		authIDSendCmd(t, conn, "AUTH SCRAM-SHA-256 "+base64.StdEncoding.EncodeToString([]byte(clientFirst)))
		resp := authIDReadLine(t, rd)
		if !strings.HasPrefix(resp, "334") {
			t.Fatalf("CONTROL FAILED (harness): server-first = %q", resp)
		}
		sfmRaw := strings.TrimSpace(strings.TrimPrefix(resp, "334"))
		sfm, err := base64.StdEncoding.DecodeString(sfmRaw)
		if err != nil {
			t.Fatalf("CONTROL FAILED (harness): server-first decode: %v", err)
		}
		combined := authIDNonceOf(string(sfm))
		if combined == "" {
			t.Fatalf("CONTROL FAILED (harness): no nonce in server-first %q", sfm)
		}
		// Proof verification happens after the password lookup, so a garbage
		// proof still exercises onGetPassword with the looked-up username.
		clientFinal := "c=biws,r=" + combined + ",p=" +
			base64.StdEncoding.EncodeToString([]byte("garbage-proof"))
		authIDSendCmd(t, conn, base64.StdEncoding.EncodeToString([]byte(clientFinal)))
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "535") {
			t.Fatalf("CONTROL FAILED (harness): expected 535 for garbage proof, got %q", resp)
		}
		if rec.passwordFor != "alice@example.com" {
			t.Fatalf("FAIL: SCRAM onGetPassword received username %q, want %q", rec.passwordFor, "alice@example.com")
		}
	})

	t.Run("onLoginResult usernames are normalized", func(t *testing.T) {
		// PLAIN failure path.
		rec := &authIDRecorder{}
		conn, rd := newAuthIDSession(t, rec)
		ir := base64.StdEncoding.EncodeToString([]byte("\x00Alice@Example.com\x00WRONG"))
		authIDSendCmd(t, conn, "AUTH PLAIN "+ir)
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "535") {
			t.Fatalf("CONTROL FAILED (harness): failed AUTH PLAIN = %q", resp)
		}
		conn.Close()
		if len(rec.loginResults) != 1 || rec.loginResults[0] != "alice@example.com" {
			t.Fatalf("FAIL: PLAIN onLoginResult usernames = %v, want [alice@example.com]", rec.loginResults)
		}

		// LOGIN success path.
		rec2 := &authIDRecorder{}
		conn2, rd2 := newAuthIDSession(t, rec2)
		authIDSendCmd(t, conn2, "AUTH LOGIN")
		authIDReadLine(t, rd2)
		authIDSendCmd(t, conn2, base64.StdEncoding.EncodeToString([]byte("Alice@Example.com")))
		authIDReadLine(t, rd2)
		authIDSendCmd(t, conn2, base64.StdEncoding.EncodeToString([]byte("secret")))
		authIDReadLine(t, rd2)
		conn2.Close()
		if len(rec2.loginResults) != 1 || rec2.loginResults[0] != "alice@example.com" {
			t.Fatalf("FAIL: LOGIN onLoginResult usernames = %v, want [alice@example.com]", rec2.loginResults)
		}
	})
}
