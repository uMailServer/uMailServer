package smtp

// F5055: the BDAT/CHUNKING path delivers the message without the trace and
// result headers the DATA path adds (Received, Authentication-Results,
// X-Spam-Score, Message-ID). RFC 5321 §4.4: an SMTP server receiving a
// message MUST insert a Received trace header; the choice of DATA vs BDAT is
// the client's and must not change what is stored.
//
// Driven through the real command loop (Server.handleConnection) over net.Pipe.

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

type regF5055Stage struct{}

func (regF5055Stage) Name() string { return "F5055" }
func (regF5055Stage) Process(ctx *MessageContext) PipelineResult {
	ctx.SPFResult = SPFResult{Result: "pass", Domain: "example.org"}
	ctx.SpamResult.Score = 1.5
	return ResultAccept
}

type regF5055Client struct {
	conn net.Conn
	r    *bufio.Reader
}

func regF5055Start(t *testing.T, p *Pipeline, delivered *[]string) *regF5055Client {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{
		Hostname:       "mx.test",
		MaxMessageSize: 1 << 20,
		MaxRecipients:  10,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
	}, nil)
	if p != nil {
		srv.SetPipeline(p)
	}
	srv.SetDeliveryHandler(func(from string, to []string, data []byte) error {
		*delivered = append(*delivered, string(data))
		return nil
	})
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	t.Cleanup(func() { _ = clientConn.Close(); <-done })
	_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
	c := &regF5055Client{conn: clientConn, r: bufio.NewReader(clientConn)}
	c.reply(t)
	return c
}

func (c *regF5055Client) reply(t *testing.T) string {
	t.Helper()
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			t.Fatalf("read reply: %v", err)
		}
		if len(line) >= 4 && line[3] == ' ' {
			return strings.TrimRight(line, "\r\n")
		}
	}
}

func (c *regF5055Client) cmd(t *testing.T, s string) string {
	t.Helper()
	if _, err := c.conn.Write([]byte(s)); err != nil {
		t.Fatalf("write: %v", err)
	}
	return c.reply(t)
}

const regF5055Msg = "From: a@example.org\r\nTo: b@mx.test\r\nSubject: hi\r\n\r\nbody\r\n"

func regF5055Send(t *testing.T, p *Pipeline, bdat bool) string {
	t.Helper()
	var delivered []string
	c := regF5055Start(t, p, &delivered)
	c.cmd(t, "EHLO client.example.org\r\n")
	c.cmd(t, "MAIL FROM:<a@example.org>\r\n")
	c.cmd(t, "RCPT TO:<b@mx.test>\r\n")
	var final string
	if bdat {
		final = c.cmd(t, fmt.Sprintf("BDAT %d LAST\r\n%s", len(regF5055Msg), regF5055Msg))
	} else {
		if r := c.cmd(t, "DATA\r\n"); !strings.HasPrefix(r, "354") {
			t.Fatalf("DATA reply %q", r)
		}
		final = c.cmd(t, regF5055Msg+".\r\n")
	}
	if !strings.HasPrefix(final, "250") || len(delivered) != 1 {
		t.Fatalf("final=%q delivered=%d", final, len(delivered))
	}
	c.cmd(t, "QUIT\r\n")
	return delivered[0]
}

func regF5055Header(data, name string) bool {
	hdr := data
	if i := strings.Index(data, "\r\n\r\n"); i >= 0 {
		hdr = data[:i+2]
	}
	return strings.HasPrefix(hdr, name+":") || strings.Contains(hdr, "\r\n"+name+":")
}

var regF5055Want = []string{"Received", "Authentication-Results", "X-Spam-Score", "Message-ID"}

func regF5055Check(t *testing.T, label, data string, want []string) {
	t.Helper()
	var missing []string
	for _, h := range want {
		if !regF5055Header(data, h) {
			missing = append(missing, h)
		}
	}
	if len(missing) > 0 {
		t.Errorf("F5055 %s: delivered message lacks %v:\n%s", label, missing, data)
	}
}

// the DATA path with a pipeline adds every header.
func TestF5055ControlData(t *testing.T) {
	p := NewPipeline(nil)
	p.AddStage(regF5055Stage{})
	data := regF5055Send(t, p, false)
	for _, h := range regF5055Want {
		if !regF5055Header(data, h) {
			t.Fatalf("control: DATA lacks %s:\n%s", h, data)
		}
	}
}

func TestF5055BdatHeaders(t *testing.T) {
	p := NewPipeline(nil)
	p.AddStage(regF5055Stage{})
	regF5055Check(t, "BDAT+pipeline", regF5055Send(t, p, true), regF5055Want)
}

// Without a pipeline (the submission servers) DATA adds only a Message-ID;
// BDAT must do the same.
func TestF5055NoPipelineMessageID(t *testing.T) {
	regF5055Check(t, "DATA/no-pipeline", regF5055Send(t, nil, false), []string{"Message-ID"})
	regF5055Check(t, "BDAT/no-pipeline", regF5055Send(t, nil, true), []string{"Message-ID"})
}

// bdatPayload returns a delivered message without the headers the session
// prepends (Message-ID, Received), i.e. the octets the client sent. BDAT
// adds the same headers as DATA since F5055.
func bdatPayload[T ~string | ~[]byte](data T) string {
	s := string(data)
	for strings.HasPrefix(s, "Message-ID: <") || strings.HasPrefix(s, "Received: from ") {
		i := strings.Index(s, "\r\n")
		if i < 0 {
			break
		}
		s = s[i+2:]
	}
	return s
}

// Edge cases.

// A stage that rewrites ctx.Data in place (the server's spamHeaderGuardStage
// pattern) must see its change delivered on both paths.
type regF5055InPlace struct{}

func (regF5055InPlace) Name() string { return "F5055InPlace" }
func (regF5055InPlace) Process(ctx *MessageContext) PipelineResult {
	if i := strings.Index(string(ctx.Data), "X-Spam-Status:"); i >= 0 {
		copy(ctx.Data[i:], "X-Orig-")
	}
	return ResultAccept
}

func TestF5055EdgeInPlaceStage(t *testing.T) {
	msg := "X-Spam-Status: No\r\nSubject: s\r\n\r\nb\r\n"
	for _, bdat := range []bool{false, true} {
		p := NewPipeline(nil)
		p.AddStage(regF5055InPlace{})
		var delivered []string
		c := regF5055Start(t, p, &delivered)
		c.cmd(t, "EHLO c.example\r\n")
		c.cmd(t, "MAIL FROM:<a@example.org>\r\n")
		c.cmd(t, "RCPT TO:<b@mx.test>\r\n")
		if bdat {
			c.cmd(t, fmt.Sprintf("BDAT 6\r\n%s", msg[:6]))
			c.cmd(t, fmt.Sprintf("BDAT %d LAST\r\n%s", len(msg)-6, msg[6:]))
		} else {
			c.cmd(t, "DATA\r\n")
			c.cmd(t, msg+".\r\n")
		}
		if len(delivered) != 1 || strings.Contains(delivered[0], "X-Spam-Status: No") || !strings.Contains(delivered[0], "X-Orig-Status: No") {
			t.Errorf("F5055 in-place stage change lost (bdat=%v): %q", bdat, delivered)
		}
	}
}

// A client Message-ID is kept and not duplicated; multi-chunk BDAT gets the
// pipeline headers too.
func TestF5055EdgeExistingMessageIDMultiChunk(t *testing.T) {
	msg := "Message-ID: <x@client>\r\nSubject: s\r\n\r\nb\r\n"
	p := NewPipeline(nil)
	p.AddStage(regF5055Stage{})
	var delivered []string
	c := regF5055Start(t, p, &delivered)
	c.cmd(t, "EHLO c.example\r\n")
	c.cmd(t, "MAIL FROM:<a@example.org>\r\n")
	c.cmd(t, "RCPT TO:<b@mx.test>\r\n")
	c.cmd(t, fmt.Sprintf("BDAT 10\r\n%s", msg[:10]))
	c.cmd(t, fmt.Sprintf("BDAT %d LAST\r\n%s", len(msg)-10, msg[10:]))
	if len(delivered) != 1 {
		t.Fatalf("delivered=%d", len(delivered))
	}
	d := delivered[0]
	if strings.Count(strings.ToLower(d), "message-id:") != 1 || !strings.Contains(d, "<x@client>") {
		t.Errorf("F5055 Message-ID handling: %q", d)
	}
	for _, h := range []string{"Received", "Authentication-Results", "X-Spam-Score"} {
		if !regF5055Header(d, h) {
			t.Errorf("F5055 multi-chunk BDAT lacks %s: %q", h, d)
		}
	}
}
