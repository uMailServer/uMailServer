package smtp

import (
	"net"
	"strings"
	"testing"
)

func r112Session(host string) *Session {
	srv := NewServer(&Config{Hostname: host, MaxMessageSize: 1 << 20, MaxRecipients: 10}, nil)
	s := NewSession(r85AddrConn{addr: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 2525}}, srv)
	s.helloDomain = "client.example"
	s.mailFrom = "bounce@esp.example"
	s.rcptTo = []string{"b@mx.test"}
	return s
}

const r112Msg = "From: Bob <bob@brand.example>\r\nSubject: x\r\n\r\nbody\r\n"

func r112AR(out string) string {
	hdr := out[:strings.Index(out, "\r\n\r\n")]
	i := strings.Index(hdr, "Authentication-Results:")
	if i < 0 {
		return ""
	}
	rest := hdr[i:]
	// include folded continuation lines
	end := len(rest)
	for j := 0; j < len(rest); j++ {
		if rest[j] == '\n' && j+1 < len(rest) && rest[j+1] != ' ' && rest[j+1] != '\t' {
			end = j
			break
		}
	}
	return rest[:end]
}

// F5940: header.from must be the RFC 5322 From domain, not the envelope sender.
func TestR112F5940HeaderFromIsAuthorDomain(t *testing.T) {
	s := r112Session("mx.test")
	ctx := &MessageContext{Headers: map[string][]string{"From": {"Bob <bob@brand.example>"}}, DMARCResult: DMARCResult{Result: "pass"}}
	ar := r112AR(string(s.addTraceHeaders(ctx, []byte(r112Msg))))
	if !strings.Contains(ar, "header.from=brand.example") || strings.Contains(ar, "bounce@") {
		t.Errorf("DEFECT F5940: header.from not author domain: %q", ar)
	}
}

// F5941: a DKIM failure reason must not inject headers into the message.
func TestR112F5941DKIMReasonInjection(t *testing.T) {
	s := r112Session("mx.test")
	ctx := &MessageContext{DKIMResult: DKIMResult{Domain: "d.example", Error: "bad\r\nX-Evil: 1\r\n\r\nbody"}}
	out := string(s.addTraceHeaders(ctx, []byte(r112Msg)))
	if strings.Contains(out, "\r\nX-Evil:") || strings.HasPrefix(out, "X-Evil:") {
		t.Errorf("DEFECT F5941: header injection via DKIM reason: %q", out)
	}
}

// F5942: the ARC verdict must persist in Authentication-Results.
func TestR112F5942ARCVerdictPersisted(t *testing.T) {
	s := r112Session("mx.test")
	ctx := &MessageContext{ARCResult: ARCResult{Result: "pass", ChainValid: true}}
	ar := r112AR(string(s.addTraceHeaders(ctx, []byte(r112Msg))))
	if !strings.Contains(ar, "arc=pass") {
		t.Errorf("DEFECT F5942: ARC verdict not in Authentication-Results: %q", ar)
	}
}

// F5944: Received must use the same defaulted hostname as Authentication-Results.
func TestR112F5944ReceivedHostnameDefault(t *testing.T) {
	s := r112Session("")
	out := string(s.addTraceHeaders(&MessageContext{}, []byte(r112Msg)))
	if strings.Contains(out, "by  with") || strings.Contains(out, "@>") {
		t.Errorf("DEFECT F5944: Received has empty 'by' host: %q", out)
	}
}

// F5943: delivery handler can receive the validated ARC result (seam).
func TestR112F5943DeliveryAuthSeam(t *testing.T) {
	s := r112Session("mx.test")
	var got *DeliveryAuth
	s.server.SetDeliveryHandlerWithAuth(func(from string, to, notify []string, data []byte, info *DeliveryAuth) error {
		got = info
		return nil
	})
	s.data = []byte(r112Msg)
	pctx := &MessageContext{ARCResult: ARCResult{Result: "pass"}, SPFResult: SPFResult{Result: "pass", Domain: "esp.example"}}
	if err := s.deliver(pctx); err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ARC.Result != "pass" || !strings.Contains(got.AuthResults, "arc=pass") {
		t.Errorf("seam did not deliver ARC info: %+v", got)
	}
	got = nil
	if err := s.deliver(nil); err != nil || got == nil {
		t.Errorf("info must be non-nil without pipeline: %v %v", err, got)
	}
}

// F5945: the SMTPUTF8 fallback of ValidateEmail must not accept malformed
// addresses (spaces, controls, several '@', delimiters).
func TestR112F5945ValidateEmailFallback(t *testing.T) {
	for _, bad := range []string{"foo bar@x.test", "a@b@c.test", "a\x00b@x.test", "a>b@x.test", "a@x.test, b@y.test", "a@x y", "a\tb@x.test", "üa b@x.test", "ü@@x.test"} {
		if v, err := ValidateEmail(bad); err == nil {
			t.Errorf("DEFECT F5945: %q accepted as %q", bad, v)
		}
	}
	for _, good := range []string{"ünï@exämple.test", "user@example.com", "用户@例え.jp"} {
		if _, err := ValidateEmail(good); err != nil {
			t.Errorf("INVALID: %q rejected: %v", good, err)
		}
	}
}
