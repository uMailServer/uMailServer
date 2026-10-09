package smtp

// F4945: a pipeline stage that rejects a message (ResultReject with
// ctx.RejectionCode 550, e.g. the relay-policy stage or the ScoreStage spam
// reject threshold) is answered with "451 4.4.0 local error" because
// Pipeline.Process returns a non-nil error alongside ResultReject and the
// session treats every error as a local temporary failure. A permanent
// rejection becomes a transient one: the sending MTA retries for days.
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

type prcStage struct {
	code   int
	msg    string
	result PipelineResult
	score  float64
}

func (f *prcStage) Name() string { return "F4945" }
func (f *prcStage) Process(ctx *MessageContext) PipelineResult {
	ctx.SpamScore += f.score
	if f.result == ResultReject {
		ctx.Rejected = true
		ctx.RejectionCode = f.code
		ctx.RejectionMessage = f.msg
	}
	return f.result
}

type prcClient struct {
	conn net.Conn
	r    *bufio.Reader
}

func prcStart(t *testing.T, p *Pipeline, delivered *int) *prcClient {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{
		Hostname:       "mx.test",
		MaxMessageSize: 1 << 20,
		MaxRecipients:  10,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
	}, nil)
	srv.SetPipeline(p)
	srv.SetDeliveryHandler(func(from string, to []string, data []byte) error {
		*delivered++
		return nil
	})
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	t.Cleanup(func() { _ = clientConn.Close(); <-done })
	_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
	c := &prcClient{conn: clientConn, r: bufio.NewReader(clientConn)}
	c.reply(t)
	return c
}

func (c *prcClient) reply(t *testing.T) string {
	t.Helper()
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			t.Fatalf("setup: read reply: %v", err)
		}
		if len(line) < 4 {
			t.Fatalf("setup: short reply %q", line)
		}
		if line[3] == ' ' {
			return strings.TrimRight(line, "\r\n")
		}
	}
}

func (c *prcClient) cmd(t *testing.T, line string) string {
	t.Helper()
	if _, err := c.conn.Write([]byte(line + "\r\n")); err != nil {
		t.Fatalf("setup: write: %v", err)
	}
	return c.reply(t)
}

func (c *prcClient) envelope(t *testing.T) {
	t.Helper()
	for _, l := range []string{"EHLO client.test", "MAIL FROM:<a@sender.test>", "RCPT TO:<b@mx.test>"} {
		if r := c.cmd(t, l); !strings.HasPrefix(r, "250") {
			t.Fatalf("setup: %s -> %s", l, r)
		}
	}
}

// prcData runs one DATA transaction and returns the final reply line.
func prcData(t *testing.T, p *Pipeline, delivered *int) string {
	c := prcStart(t, p, delivered)
	c.envelope(t)
	if r := c.cmd(t, "DATA"); !strings.HasPrefix(r, "354") {
		t.Fatalf("setup: DATA -> %s", r)
	}
	return c.cmd(t, "Subject: x\r\n\r\nbody\r\n.")
}

// prcBdat runs one BDAT LAST transaction and returns the final reply line.
func prcBdat(t *testing.T, p *Pipeline, delivered *int) string {
	c := prcStart(t, p, delivered)
	c.envelope(t)
	body := "Subject: x\r\n\r\nbody\r\n"
	if _, err := c.conn.Write([]byte(fmt.Sprintf("BDAT %d LAST\r\n%s", len(body), body))); err != nil {
		t.Fatalf("setup: write: %v", err)
	}
	return c.reply(t)
}

func prcPipe(stages ...PipelineStage) *Pipeline {
	p := NewPipeline(nil)
	for _, s := range stages {
		p.AddStage(s)
	}
	return p
}

func TestPipelineRejectCodeControl(t *testing.T) {
	var delivered int
	r := prcData(t, prcPipe(&prcStage{result: ResultAccept}), &delivered)
	r2 := prcBdat(t, prcPipe(&prcStage{result: ResultAccept}), &delivered)
	t.Logf("control: DATA %q BDAT %q delivered=%d", r, r2, delivered)
	if !strings.HasPrefix(r, "250") || !strings.HasPrefix(r2, "250") || delivered != 2 {
		t.Fatalf("control failed: %q %q delivered=%d", r, r2, delivered)
	}
}

func prcCheck(t *testing.T, name, got, wantPrefix string, delivered int) {
	t.Logf("EXPECTED: %s...  delivered=0", wantPrefix)
	t.Logf("ACTUAL:   %q delivered=%d", got, delivered)
	if !strings.HasPrefix(got, wantPrefix) || delivered != 0 {
		t.Errorf("F4945 (%s): stage rejection answered %q, want %s", name, got, wantPrefix)
	}
}

func TestPipelineRejectCodeRelayRejectData(t *testing.T) {
	var d int
	got := prcData(t, prcPipe(&prcStage{result: ResultReject, code: 550, msg: "5.7.1 Relaying denied"}), &d)
	prcCheck(t, "DATA relay 550", got, "550 5.7.1 Relaying denied", d)
}

func TestPipelineRejectCodeRelayRejectBdat(t *testing.T) {
	var d int
	got := prcBdat(t, prcPipe(&prcStage{result: ResultReject, code: 550, msg: "5.7.1 Relaying denied"}), &d)
	prcCheck(t, "BDAT relay 550", got, "550 5.7.1 Relaying denied", d)
}

func TestPipelineRejectCodeSpamScoreReject(t *testing.T) {
	var d int
	got := prcData(t, prcPipe(&prcStage{result: ResultAccept, score: 20}, NewScoreStage(10, 5)), &d)
	prcCheck(t, "ScoreStage reject", got, "550", d)
}

// Edge: a stage-supplied temporary code (greylisting, 451) is preserved.
func TestPipelineRejectCodeEdgeTempCodePreserved(t *testing.T) {
	var d int
	got := prcData(t, prcPipe(&prcStage{result: ResultReject, code: 451, msg: "4.7.1 Greylisted, please try again later"}), &d)
	prcCheck(t, "greylist 451", got, "451 4.7.1 Greylisted", d)
}

// Edge: a reject without a code falls back to a permanent 550.
func TestPipelineRejectCodeEdgeNoCodeDefaults550(t *testing.T) {
	var d int
	got := prcData(t, prcPipe(&prcStage{result: ResultReject}), &d)
	prcCheck(t, "no code", got, "550", d)
	var d2 int
	got2 := prcBdat(t, prcPipe(&prcStage{result: ResultReject}), &d2)
	prcCheck(t, "no code BDAT", got2, "550", d2)
}

// Edge: stages after the rejecting stage do not run; session stays usable.
func TestPipelineRejectCodeEdgeLaterStageSkippedAndSessionUsable(t *testing.T) {
	var d int
	later := &prcStage{result: ResultAccept}
	calls := 0
	p := prcPipe(&prcStage{result: ResultReject, code: 554, msg: "5.7.1 no"}, prcStageFunc(func(*MessageContext) PipelineResult { calls++; return later.result }))
	c := prcStart(t, p, &d)
	c.envelope(t)
	c.cmd(t, "DATA")
	got := c.cmd(t, "Subject: x\r\n\r\nbody\r\n.")
	prcCheck(t, "554", got, "554 5.7.1 no", d)
	if calls != 0 {
		t.Errorf("F4945: stage after reject ran %d times", calls)
	}
	if r := c.cmd(t, "RCPT TO:<b@mx.test>"); !strings.HasPrefix(r, "503") {
		t.Errorf("F4945: transaction not reset after reject: %q", r)
	}
}

type prcStageFunc func(*MessageContext) PipelineResult

func (f prcStageFunc) Name() string                               { return "func" }
func (f prcStageFunc) Process(ctx *MessageContext) PipelineResult { return f(ctx) }
