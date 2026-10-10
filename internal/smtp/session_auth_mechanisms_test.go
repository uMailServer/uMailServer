package smtp

// Regression tests for SMTP AUTH mechanism defects (evidence-audit round 51):
//   - F5320: SCRAM-SHA-256 built AuthMessage from the full client-first
//     (with GS2 header) and full client-final (with p=) and compared the
//     proof to ClientSignature, so no RFC 5802/7677 client could log in.
//   - F5321: SCRAM-SHA-256 was advertised without a password source (the
//     production wiring) and each attempt was charged to the lockout.
//   - F5322: AUTH LOGIN ignored the initial response and took the next line
//     (the password) as the user name, reporting it to onLoginResult.

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func authMech5320HMAC(key []byte, msg string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return h.Sum(nil)
}

// authMech5320ClientFinal computes the RFC 5802 client-final-message and the
// ServerSignature the client expects in server-final.
func authMech5320ClientFinal(t *testing.T, password, gs2, clientFirstBare, serverFirst string) (string, []byte) {
	t.Helper()
	var nonce, saltB64, iterS string
	for _, f := range strings.Split(serverFirst, ",") {
		switch {
		case strings.HasPrefix(f, "r="):
			nonce = f[2:]
		case strings.HasPrefix(f, "s="):
			saltB64 = f[2:]
		case strings.HasPrefix(f, "i="):
			iterS = f[2:]
		}
	}
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		t.Fatalf("CONTROL FAILED (client): salt %q: %v", saltB64, err)
	}
	iter, err := strconv.Atoi(iterS)
	if err != nil {
		t.Fatalf("CONTROL FAILED (client): iterations %q", iterS)
	}
	salted, err := pbkdf2.Key(sha256.New, password, salt, iter, sha256.Size)
	if err != nil {
		t.Fatalf("CONTROL FAILED (client): pbkdf2: %v", err)
	}
	clientKey := authMech5320HMAC(salted, "Client Key")
	storedKey := sha256.Sum256(clientKey)
	serverKey := authMech5320HMAC(salted, "Server Key")
	withoutProof := "c=" + base64.StdEncoding.EncodeToString([]byte(gs2)) + ",r=" + nonce
	authMessage := clientFirstBare + "," + serverFirst + "," + withoutProof
	clientSig := authMech5320HMAC(storedKey[:], authMessage)
	proof := make([]byte, len(clientKey))
	for i := range proof {
		proof[i] = clientKey[i] ^ clientSig[i]
	}
	return withoutProof + ",p=" + base64.StdEncoding.EncodeToString(proof), authMech5320HMAC(serverKey, authMessage)
}

// authMech5320Exchange runs one SCRAM-SHA-256 exchange against a session whose
// password handler returns "secret". It returns the final reply line and the
// ServerSignature the client expects.
func authMech5320Exchange(t *testing.T, conn net.Conn, rdLine func() string, password, gs2, bare string, sendIR bool, tamper bool) (string, []byte) {
	t.Helper()
	clientFirst := gs2 + bare
	if sendIR {
		authIDSendCmd(t, conn, "AUTH SCRAM-SHA-256 "+base64.StdEncoding.EncodeToString([]byte(clientFirst)))
	} else {
		authIDSendCmd(t, conn, "AUTH SCRAM-SHA-256")
		if resp := rdLine(); !strings.HasPrefix(resp, "334") {
			t.Fatalf("CONTROL FAILED (harness): empty challenge = %q", resp)
		}
		authIDSendCmd(t, conn, base64.StdEncoding.EncodeToString([]byte(clientFirst)))
	}
	resp := rdLine()
	if !strings.HasPrefix(resp, "334 ") {
		t.Fatalf("CONTROL FAILED (harness): server-first = %q", resp)
	}
	sf, err := base64.StdEncoding.DecodeString(strings.TrimSpace(resp[4:]))
	if err != nil {
		t.Fatalf("CONTROL FAILED (harness): server-first b64: %v", err)
	}
	final, wantSig := authMech5320ClientFinal(t, password, gs2, bare, string(sf))
	if tamper {
		i := strings.LastIndex(final, ",p=") + 3
		p, _ := base64.StdEncoding.DecodeString(final[i:])
		p[0] ^= 1
		final = final[:i] + base64.StdEncoding.EncodeToString(p)
	}
	authIDSendCmd(t, conn, base64.StdEncoding.EncodeToString([]byte(final)))
	return rdLine(), wantSig
}

// Control: the independent client reproduces the RFC 7677 §3 test vector, and
// the session harness authenticates the same account with PLAIN.
func TestSCRAMClientMatchesRFC7677Vector(t *testing.T) {
	sf := "r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096"
	final, sig := authMech5320ClientFinal(t, "pencil", "n,,", "n=user,r=rOprNGfwEbeRWgbNEkqO", sf)
	wantFinal := "c=biws,r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,p=dHzbZapWIk4jUhN+Ute9ytag9zjfMHgsqmmiz7AndVQ="
	wantSig := "6rriTRBi23WpRR/wtup+mMhUZUn/dB5nLTJRsjl95G4="
	if final != wantFinal || base64.StdEncoding.EncodeToString(sig) != wantSig {
		t.Fatalf("CONTROL FAILED: client does not match RFC 7677 vector:\n final=%s\n sig=%s", final, base64.StdEncoding.EncodeToString(sig))
	}
	t.Logf("client matches RFC 7677 test vector (p=dHzbZa..., v=6rriTR...)")

	conn, rd := newAuthIDSession(t, &authIDRecorder{})
	defer conn.Close()
	authIDSendCmd(t, conn, "AUTH PLAIN "+base64.StdEncoding.EncodeToString([]byte("\x00alice@example.com\x00secret")))
	if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "235") {
		t.Fatalf("CONTROL FAILED: PLAIN = %q", resp)
	}
}

// Failure: an RFC-compliant SCRAM-SHA-256 client with the right password.
func TestSCRAMSHA256CompliantClientAuthenticates(t *testing.T) {
	conn, rd := newAuthIDSession(t, &authIDRecorder{})
	defer conn.Close()
	resp, wantSig := authMech5320Exchange(t, conn, func() string { return authIDReadLine(t, rd) },
		"secret", "n,,", "n=alice@example.com,r=fyko+d2lbbFgONRv9qkxdawL", true, false)
	t.Logf("EXPECTED: 235 v=%s", base64.StdEncoding.EncodeToString(wantSig))
	t.Logf("ACTUAL:   %s", resp)
	if !strings.HasPrefix(resp, "235") {
		t.Fatalf("REGRESSION F5320: compliant SCRAM-SHA-256 client with correct password rejected: %q", resp)
	}
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(resp, "235"), " "))
	got, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, "v="))
	if !strings.HasPrefix(v, "v=") || err != nil || !bytes.Equal(got, wantSig) {
		t.Fatalf("REGRESSION F5320: server-final %q does not carry the RFC 5802 ServerSignature", v)
	}
}

// Verify: edges around the fixed verification.
func TestSCRAMSHA256ProofEdges(t *testing.T) {
	cases := []struct {
		name     string
		password string
		gs2      string
		bare     string
		ir       bool
		tamper   bool
		want     string
	}{
		{"wrong password rejected", "WRONG", "n,,", "n=alice@example.com,r=abc123", true, false, "535"},
		{"tampered proof rejected", "secret", "n,,", "n=alice@example.com,r=abc123", true, true, "535"},
		{"authzid in gs2 header", "secret", "n,a=alice@example.com,", "n=alice@example.com,r=abc123", true, false, "235"},
		{"no initial response (334 empty challenge)", "secret", "n,,", "n=alice@example.com,r=xyz789", false, false, "235"},
		{"mixed-case username", "secret", "n,,", "n=Alice@Example.COM,r=q1w2e3", true, false, "235"},
		{"y flag gs2 header", "secret", "y,,", "n=alice@example.com,r=q1w2e3", true, false, "235"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, rd := newAuthIDSession(t, &authIDRecorder{})
			defer conn.Close()
			resp, _ := authMech5320Exchange(t, conn, func() string { return authIDReadLine(t, rd) },
				tc.password, tc.gs2, tc.bare, tc.ir, tc.tamper)
			if !strings.HasPrefix(resp, tc.want) {
				t.Fatalf("got %q, want %s", resp, tc.want)
			}
		})
	}
}

// authMech5321Session mirrors the production submission wiring.
func authMech5321Session(t *testing.T) (*Server, net.Conn, *bufio.Reader, []string) {
	t.Helper()
	c1, c2 := net.Pipe()
	srv := NewServer(&Config{IsSubmission: true, AllowInsecure: true, Hostname: "mx.test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetAuthHandler(func(username, password string) (bool, error) {
		return username == "alice@example.com" && password == "secret", nil
	})
	srv.SetAuthLimits(2, time.Hour)
	session := NewSession(c1, srv)
	go func() {
		rd1 := bufio.NewReader(c1)
		session.reader = rd1
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
	var ehlo []string
	for {
		line := authIDReadLine(t, rd)
		ehlo = append(ehlo, line)
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}
	t.Cleanup(func() { _ = c2.Close() })
	return srv, c2, rd, ehlo
}

func authMech5321AuthLine(ehlo []string) string {
	for _, l := range ehlo {
		if strings.Contains(l, "AUTH ") {
			return l
		}
	}
	return ""
}

// authMech5321TrySCRAM runs a client-side SCRAM attempt as far as the server
// lets it and returns the final reply.
func authMech5321TrySCRAM(t *testing.T, conn net.Conn, rd *bufio.Reader) string {
	t.Helper()
	authIDSendCmd(t, conn, "AUTH SCRAM-SHA-256 "+base64.StdEncoding.EncodeToString([]byte("n,,n=alice@example.com,r=abcdef")))
	resp := authIDReadLine(t, rd)
	if !strings.HasPrefix(resp, "334 ") {
		return resp
	}
	sf, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(resp[4:]))
	final, _ := authMech5320ClientFinal(t, "secret", "n,,", "n=alice@example.com,r=abcdef", string(sf))
	authIDSendCmd(t, conn, base64.StdEncoding.EncodeToString([]byte(final)))
	return authIDReadLine(t, rd)
}

// Control: PLAIN, which the server can verify, is advertised and succeeds.
func TestAuthPLAINWithoutPasswordHandler(t *testing.T) {
	_, conn, rd, ehlo := authMech5321Session(t)
	if !strings.Contains(authMech5321AuthLine(ehlo), "PLAIN") {
		t.Fatalf("CONTROL FAILED: PLAIN not advertised: %v", ehlo)
	}
	authIDSendCmd(t, conn, "AUTH PLAIN "+base64.StdEncoding.EncodeToString([]byte("\x00alice@example.com\x00secret")))
	if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "235") {
		t.Fatalf("CONTROL FAILED: PLAIN = %q", resp)
	}
}

// Failure: a client that picks the advertised SCRAM-SHA-256 (e.g. msmtp /
// libgsasl prefer it over PLAIN) can never authenticate, and the attempt is
// charged to the IP's brute-force lockout counter.
func TestSCRAMNotAdvertisedWithoutPasswordHandler(t *testing.T) {
	srv, conn, rd, ehlo := authMech5321Session(t)
	authLine := authMech5321AuthLine(ehlo)
	advertised := strings.Contains(authLine, "SCRAM-SHA-256")
	r1 := authMech5321TrySCRAM(t, conn, rd)
	r2 := authMech5321TrySCRAM(t, conn, rd)
	locked := srv.isAuthLockedOut("pipe")
	authIDSendCmd(t, conn, "AUTH PLAIN "+base64.StdEncoding.EncodeToString([]byte("\x00alice@example.com\x00secret")))
	plain := authIDReadLine(t, rd)
	t.Logf("EXPECTED: EHLO AUTH line without SCRAM-SHA-256 (no password source) or SCRAM succeeds; no lockout; later PLAIN 235")
	t.Logf("ACTUAL:   EHLO %q; SCRAM #1 %q; SCRAM #2 %q; locked=%v; PLAIN %q", authLine, r1, r2, locked, plain)
	if advertised && !strings.HasPrefix(r1, "235") {
		t.Fatalf("REGRESSION F5321: SCRAM-SHA-256 advertised but cannot complete (%q)", r1)
	}
	if !strings.HasPrefix(plain, "235") {
		t.Fatalf("REGRESSION F5321: SCRAM attempts locked out the correct PLAIN login: %q", plain)
	}
}

// Verify: no SCRAM without a password handler, 504 without lockout charge,
// SCRAM still advertised when a handler is wired, other mechanisms intact.
func TestSCRAMUnavailableEdges(t *testing.T) {
	t.Run("rejected 504 before any exchange, not charged", func(t *testing.T) {
		srv, conn, rd, _ := authMech5321Session(t)
		for i := 0; i < 3; i++ {
			authIDSendCmd(t, conn, "AUTH SCRAM-SHA-256")
			if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "504") {
				t.Fatalf("attempt %d: got %q, want 504", i, resp)
			}
		}
		if srv.isAuthLockedOut("pipe") {
			t.Fatal("unsupported mechanism attempts charged to lockout")
		}
	})
	t.Run("advertised when password handler wired", func(t *testing.T) {
		rec := &authIDRecorder{}
		c1, c2 := net.Pipe()
		defer c2.Close()
		srv := NewServer(&Config{IsSubmission: true, AllowInsecure: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		srv.SetPasswordHandler(func(string) (string, error) { rec.passwordFor = "x"; return "secret", nil })
		session := NewSession(c1, srv)
		go func() { _ = session.HandleCommand("EHLO x") }()
		rd := bufio.NewReader(c2)
		var ehlo []string
		for {
			l := authIDReadLine(t, rd)
			ehlo = append(ehlo, l)
			if strings.HasPrefix(l, "250 ") {
				break
			}
		}
		if !strings.Contains(authMech5321AuthLine(ehlo), "SCRAM-SHA-256") {
			t.Fatalf("SCRAM-SHA-256 not advertised with password handler: %v", ehlo)
		}
	})
	t.Run("LOGIN still advertised and works", func(t *testing.T) {
		_, conn, rd, ehlo := authMech5321Session(t)
		if !strings.Contains(authMech5321AuthLine(ehlo), "LOGIN") {
			t.Fatalf("LOGIN missing: %v", ehlo)
		}
		authIDSendCmd(t, conn, "AUTH LOGIN")
		authIDReadLine(t, rd)
		authIDSendCmd(t, conn, base64.StdEncoding.EncodeToString([]byte("alice@example.com")))
		authIDReadLine(t, rd)
		authIDSendCmd(t, conn, base64.StdEncoding.EncodeToString([]byte("secret")))
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "235") {
			t.Fatalf("LOGIN = %q", resp)
		}
	})
}

func authMech5322B64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// Control: LOGIN without initial response succeeds.
func TestAuthLOGINWithoutInitialResponse(t *testing.T) {
	conn, rd := newAuthIDSession(t, &authIDRecorder{})
	defer conn.Close()
	authIDSendCmd(t, conn, "AUTH LOGIN")
	if resp := authIDReadLine(t, rd); resp != "334 VXNlcm5hbWU6" {
		t.Fatalf("CONTROL FAILED: %q", resp)
	}
	authIDSendCmd(t, conn, authMech5322B64("alice@example.com"))
	if resp := authIDReadLine(t, rd); resp != "334 UGFzc3dvcmQ6" {
		t.Fatalf("CONTROL FAILED: %q", resp)
	}
	authIDSendCmd(t, conn, authMech5322B64("secret"))
	if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "235") {
		t.Fatalf("CONTROL FAILED: %q", resp)
	}
}

// Failure: the user name is supplied as the initial response; the next
// server challenge must ask for the password.
func TestAuthLOGINInitialResponse(t *testing.T) {
	rec := &authIDRecorder{}
	conn, rd := newAuthIDSession(t, rec)
	defer conn.Close()
	authIDSendCmd(t, conn, "AUTH LOGIN "+authMech5322B64("alice@example.com"))
	first := authIDReadLine(t, rd)
	// Behave like smtplib: every following challenge is answered with the password.
	authIDSendCmd(t, conn, authMech5322B64("secret"))
	second := authIDReadLine(t, rd)
	if strings.HasPrefix(second, "334") {
		authIDSendCmd(t, conn, authMech5322B64("secret"))
		second = authIDReadLine(t, rd)
	}
	t.Logf("EXPECTED: 334 UGFzc3dvcmQ6 (Password:) then 235")
	t.Logf("ACTUAL:   %q then %q; onLoginResult users=%v", first, second, rec.loginResults)
	if first != "334 UGFzc3dvcmQ6" || !strings.HasPrefix(second, "235") {
		t.Fatalf("REGRESSION F5322: AUTH LOGIN initial response ignored (%q, %q)", first, second)
	}
}

// Verify: edges of the initial-response handling.
func TestAuthLOGINInitialResponseEdges(t *testing.T) {
	t.Run("initial response with wrong password", func(t *testing.T) {
		conn, rd := newAuthIDSession(t, &authIDRecorder{})
		defer conn.Close()
		authIDSendCmd(t, conn, "AUTH LOGIN "+authMech5322B64("alice@example.com"))
		if resp := authIDReadLine(t, rd); resp != "334 UGFzc3dvcmQ6" {
			t.Fatalf("got %q", resp)
		}
		authIDSendCmd(t, conn, authMech5322B64("WRONG"))
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "535") {
			t.Fatalf("got %q", resp)
		}
	})
	t.Run("invalid base64 initial response", func(t *testing.T) {
		conn, rd := newAuthIDSession(t, &authIDRecorder{})
		defer conn.Close()
		authIDSendCmd(t, conn, "AUTH LOGIN !!!notbase64")
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "501") {
			t.Fatalf("got %q", resp)
		}
		// Session still in command mode.
		authIDSendCmd(t, conn, "NOOP")
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "250") {
			t.Fatalf("NOOP got %q", resp)
		}
	})
	t.Run("cancel at password prompt", func(t *testing.T) {
		conn, rd := newAuthIDSession(t, &authIDRecorder{})
		defer conn.Close()
		authIDSendCmd(t, conn, "AUTH LOGIN "+authMech5322B64("alice@example.com"))
		authIDReadLine(t, rd)
		authIDSendCmd(t, conn, "*")
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "501") {
			t.Fatalf("cancel got %q", resp)
		}
	})
	t.Run("mixed-case initial user normalized", func(t *testing.T) {
		rec := &authIDRecorder{}
		conn, rd := newAuthIDSession(t, rec)
		defer conn.Close()
		authIDSendCmd(t, conn, "AUTH LOGIN "+authMech5322B64("Alice@Example.com"))
		authIDReadLine(t, rd)
		authIDSendCmd(t, conn, authMech5322B64("secret"))
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "235") {
			t.Fatalf("got %q", resp)
		}
		if len(rec.loginResults) != 1 || rec.loginResults[0] != "alice@example.com" {
			t.Fatalf("login results %v", rec.loginResults)
		}
	})
}

// Malformed SCRAM messages are refused with 501 and leave the session usable.
func TestSCRAMSHA256MalformedMessages(t *testing.T) {
	t.Run("client-first without GS2 header", func(t *testing.T) {
		conn, rd := newAuthIDSession(t, &authIDRecorder{})
		defer conn.Close()
		authIDSendCmd(t, conn, "AUTH SCRAM-SHA-256 "+base64.StdEncoding.EncodeToString([]byte("n=alice@example.com,r=abc")))
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "501") {
			t.Fatalf("got %q, want 501", resp)
		}
	})
	t.Run("client-final with p= first", func(t *testing.T) {
		conn, rd := newAuthIDSession(t, &authIDRecorder{})
		defer conn.Close()
		authIDSendCmd(t, conn, "AUTH SCRAM-SHA-256 "+base64.StdEncoding.EncodeToString([]byte("n,,n=alice@example.com,r=abc")))
		resp := authIDReadLine(t, rd)
		sf, err := base64.StdEncoding.DecodeString(strings.TrimSpace(resp[4:]))
		if err != nil {
			t.Fatalf("server-first %q: %v", resp, err)
		}
		final := "p=" + base64.StdEncoding.EncodeToString(make([]byte, 32)) + ",c=biws,r=" + authIDNonceOf(string(sf))
		authIDSendCmd(t, conn, base64.StdEncoding.EncodeToString([]byte(final)))
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "501") {
			t.Fatalf("got %q, want 501", resp)
		}
		authIDSendCmd(t, conn, "NOOP")
		if resp := authIDReadLine(t, rd); !strings.HasPrefix(resp, "250") {
			t.Fatalf("NOOP got %q", resp)
		}
	})
}
