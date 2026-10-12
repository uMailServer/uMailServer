package queue

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// failureDSNBody returns the body of the first null-sender DSN queued to the sender.
func f7310DSNBody(t *testing.T, m *Manager) string {
	t.Helper()
	var body string
	_ = m.db.ForEach(db.BucketQueue, func(_ string, v []byte) error {
		var q db.QueueEntry
		if json.Unmarshal(v, &q) == nil && q.From == "" && len(q.To) == 1 && q.To[0] == "a@sender.test" {
			b, err := os.ReadFile(q.MessagePath)
			if err != nil {
				t.Fatal(err)
			}
			body = string(b)
		}
		return nil
	})
	return body
}

const f7310Msg = "Subject: s\r\nFrom: a@sender.test\r\n\r\nSECRETBODY\r\n"

func f7310Bounce(t *testing.T, params *DSNParams) string {
	t.Helper()
	m := routeManager(t, routeResolver{"remote.test": {"mx.remote.test"}}, func(string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	})
	m.maxRetries = 1
	id, err := m.EnqueueWithDSN("a@sender.test", []string{"b@remote.test"}, []string{""}, params, []byte(f7310Msg))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := m.db.GetQueueEntry(id + "-0")
	m.deliver(t.Context(), e)
	return f7310DSNBody(t, m)
}

// F7310: ENVID, ORCPT and RET reach the failure DSN (RFC 3461 §4, RFC 3464).
func TestF7310DSNCarriesEnvIDOrcptRet(t *testing.T) {
	// Control: without parameters the DSN has neither field and returns full.
	ctl := f7310Bounce(t, nil)
	if ctl == "" || strings.Contains(ctl, "Original-Envelope-Id") || strings.Contains(ctl, "Original-Recipient") || !strings.Contains(ctl, "SECRETBODY") {
		t.Fatalf("control DSN wrong:\n%s", ctl)
	}
	got := f7310Bounce(t, &DSNParams{Ret: "HDRS", EnvID: "abc+2B1", ORCPT: []string{"rfc822;orig+2Bx@remote.test"}})
	for _, want := range []string{"Original-Envelope-Id: abc+1\r\n", "Original-Recipient: rfc822; orig+x@remote.test\r\n", "Final-Recipient: rfc822; b@remote.test"} {
		if !strings.Contains(got, want) {
			t.Errorf("DSN lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "SECRETBODY") {
		t.Errorf("RET=HDRS returned the body:\n%s", got)
	}
	if !strings.Contains(got, "Subject: s") {
		t.Errorf("RET=HDRS dropped the headers:\n%s", got)
	}
	full := f7310Bounce(t, &DSNParams{Ret: "FULL", EnvID: "e1"})
	if !strings.Contains(full, "SECRETBODY") || !strings.Contains(full, "Original-Envelope-Id: e1") {
		t.Errorf("RET=FULL DSN wrong:\n%s", full)
	}
}

func TestF7310XtextDecode(t *testing.T) {
	for in, want := range map[string]string{"abc": "abc", "a+2Bb": "a+b", "+": "+", "+2": "+2", "+ZZ": "+ZZ", "a+0D+0Ab": "a\r\nb"} {
		if got := xtextDecode(in); got != want {
			t.Errorf("xtextDecode(%q)=%q want %q", in, got, want)
		}
	}
	// A decoded CRLF must not inject DSN fields.
	d, _ := GenerateDSN(&DSN{ReportedDomain: "h", OriginalFrom: "a@b", OriginalTo: "c@d", Recipient: DSNRecipient{Original: "c@d"}, EnvID: "x+0D+0AInjected: 1"}, []byte("S: 1\r\n\r\nb"), DSNRetFull)
	if strings.Contains(string(d), "\r\nInjected:") {
		t.Errorf("ENVID injected a field:\n%s", d)
	}
}
