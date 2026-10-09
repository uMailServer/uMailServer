package smtp

// F5059: an inbound message may carry an Authentication-Results header that
// claims this server's authserv-id ("mx.test; dkim=pass ... dmarc=pass").
// The session prepends its own Authentication-Results but leaves the forged
// one in place, so the stored message carries two verdicts under our name and
// a reader (MUA, filter) can trust the attacker's. RFC 8601 §5: a border MTA
// MUST delete or otherwise obscure such fields before adding its own.

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

type regF5059Stage struct{}

func (regF5059Stage) Name() string { return "F5059" }
func (regF5059Stage) Process(ctx *MessageContext) PipelineResult {
	ctx.SPFResult = SPFResult{Result: "fail", Domain: "paypal.example"}
	return ResultAccept
}

func regF5059Send(t *testing.T, bdat bool, msg string) string {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, nil)
	p := NewPipeline(nil)
	p.AddStage(regF5059Stage{})
	srv.SetPipeline(p)
	var delivered []string
	srv.SetDeliveryHandler(func(_ string, _ []string, d []byte) error {
		delivered = append(delivered, string(d))
		return nil
	})
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	defer func() { _ = clientConn.Close(); <-done }()
	_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(clientConn)
	reply := func() string {
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if len(l) >= 4 && l[3] == ' ' {
				return l[:3]
			}
		}
	}
	cmd := func(s string) string { _, _ = clientConn.Write([]byte(s)); return reply() }
	reply()
	cmd("EHLO c.example\r\n")
	cmd("MAIL FROM:<a@paypal.example>\r\n")
	cmd("RCPT TO:<b@mx.test>\r\n")
	var code string
	if bdat {
		code = cmd(fmt.Sprintf("BDAT %d LAST\r\n%s", len(msg), msg))
	} else {
		cmd("DATA\r\n")
		code = cmd(msg + ".\r\n")
	}
	if code != "250" || len(delivered) != 1 {
		t.Fatalf("code=%s delivered=%d", code, len(delivered))
	}
	cmd("QUIT\r\n")
	return delivered[0]
}

func regF5059Header(data string) string {
	if i := strings.Index(data, "\r\n\r\n"); i >= 0 {
		return data[:i+2]
	}
	return data
}

const regF5059Forged = "Authentication-Results: mx.test;\r\n\tdkim=pass header.d=paypal.example;\r\n\tdmarc=pass header.from=paypal.example\r\n" +
	"From: a@paypal.example\r\nSubject: verify\r\n\r\nbody\r\n"

// an Authentication-Results from another authserv-id is left alone
// (RFC 8601 §5 only concerns fields claiming our own id).
func TestF5059ControlOtherAuthserv(t *testing.T) {
	msg := "Authentication-Results: relay.other.example; spf=pass\r\nFrom: a@paypal.example\r\n\r\nbody\r\n"
	hdr := regF5059Header(regF5059Send(t, false, msg))
	if !strings.Contains(hdr, "Authentication-Results: relay.other.example; spf=pass") {
		t.Fatalf("control: foreign AR missing:\n%s", hdr)
	}
	if !strings.Contains(hdr, "spf=fail") {
		t.Fatalf("control: own AR missing:\n%s", hdr)
	}
}

func TestF5059ForgedOwnAuthserv(t *testing.T) {
	for _, bdat := range []bool{false, true} {
		hdr := regF5059Header(regF5059Send(t, bdat, regF5059Forged))
		label := map[bool]string{false: "DATA", true: "BDAT"}[bdat]
		forged := strings.Contains(hdr, "dkim=pass header.d=paypal.example") || strings.Contains(hdr, "dmarc=pass")
		if forged {
			t.Errorf("F5059 %s: forged Authentication-Results under our authserv-id kept:\n%s", label, hdr)
		}
	}
}

// Edge cases.

func TestF5059EdgeCaseFoldBody(t *testing.T) {
	msg := "authentication-results: MX.TEST; dkim=pass header.d=a.example\r\n" +
		"Authentication-Results:\r\n mx.test; dmarc=pass\r\n" +
		"Authentication-Results: relay.other.example; arc=pass\r\n" +
		"Subject: s\r\n\r\nquoted:\r\nAuthentication-Results: mx.test; dkim=pass\r\n"
	d := regF5059Send(t, true, msg)
	hdr := regF5059Header(d)
	if strings.Contains(hdr, "dkim=pass") || strings.Contains(hdr, "dmarc=pass") {
		t.Errorf("F5059 case-insensitive/folded forgery kept:\n%s", hdr)
	}
	if !strings.Contains(hdr, "relay.other.example; arc=pass") || !strings.Contains(hdr, "Subject: s") {
		t.Errorf("F5059 unrelated headers lost:\n%s", hdr)
	}
	if !strings.HasSuffix(d, "\r\n\r\nquoted:\r\nAuthentication-Results: mx.test; dkim=pass\r\n") {
		t.Errorf("F5059 body altered:\n%q", d)
	}
}
