package smtp

import (
	"regexp"
	"strings"
	"testing"
)

type f7312Stage struct {
	code int
	msg  string
}

func (*f7312Stage) Name() string { return "F7312" }
func (s *f7312Stage) Process(ctx *MessageContext) PipelineResult {
	ctx.Rejected, ctx.RejectionCode, ctx.RejectionMessage = true, s.code, s.msg
	return ResultReject
}

var f7312Enhanced = regexp.MustCompile(`^\d{3} [245]\.\d{1,3}\.\d{1,3} `)

// F7312: a stage rejection after DATA is answered with a code class that
// matches its meaning and an RFC 2034 enhanced status code. 421 means the
// channel is closing (RFC 5321 §4.2.2), which a stage rejection never does,
// so it is answered 451.
func TestF7312StageRejectReply(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      int
		msg       string
		wantClass byte
		wantCode  string // "" = keep code
		wantText  string // substring of the reply text
	}{
		{"rate limit 421", 421, "Rate limit exceeded", '4', "451", "Rate limit exceeded"},
		{"greylist 451", 451, "Greylisted, please try again later", '4', "451", "Greylisted"},
		{"spam 550", 550, "Message rejected as spam", '5', "550", "rejected as spam"},
		{"default", 0, "", '5', "550", "rejected"},
		{"already enhanced", 554, "5.6.0 Multiple From header fields", '5', "554", "5.6.0 Multiple"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPipeline(nil)
			p.AddStage(&f7312Stage{tc.code, tc.msg})
			c := r85Start(t, 1<<20, p, nil)
			c.txn(t, "b@mx.test")
			if r := c.cmd(t, "DATA\r\n"); !strings.HasPrefix(r, "354") {
				t.Fatalf("DATA: %q", r)
			}
			r := strings.TrimSpace(c.cmd(t, "Subject: x\r\n\r\nbody\r\n.\r\n"))
			if !f7312Enhanced.MatchString(r) || r[0] != tc.wantClass || !strings.HasPrefix(r, tc.wantCode) || !strings.Contains(r, tc.wantText) {
				t.Fatalf("reply %q: want code %s class %c, enhanced status and %q", r, tc.wantCode, tc.wantClass, tc.wantText)
			}
			if strings.Count(r, "5.6.0") > 1 {
				t.Fatalf("enhanced code duplicated: %q", r)
			}
		})
	}
}
