package imap

import (
	"errors"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func round91Msg(tag string, n int) string {
	return "From: a@example.com\r\nTo: b@example.com\r\nSubject: " + tag + "\r\n\r\n" + strings.Repeat(tag+"x", n/(len(tag)+1)) + "\r\n"
}

// F5730: APPEND must be refused with [OVERQUOTA] once the account quota is
// reached; EXPUNGE must give the bytes back.
func TestRound91AppendQuotaAndExpunge(t *testing.T) {
	m1, m2 := round91Msg("one", 1000), round91Msg("two", 1000)
	c, ms := round82Server(t, m1)
	ms.SetQuotaLimitFunc(func(user string) int64 { return 1800 })

	out := round82Append(t, c, "q1", "INBOX", m2)
	if got := round75Tagged(out); !strings.HasPrefix(got, "q1 NO [OVERQUOTA]") {
		t.Fatalf("DEFECT F5730: APPEND over quota replied %q", got)
	}
	// control: unlimited account accepts the same message
	ms.SetQuotaLimitFunc(func(string) int64 { return 0 })
	if got := round75Tagged(round82Append(t, c, "q2", "INBOX", m2)); !strings.HasPrefix(got, "q2 OK") {
		t.Fatalf("control append failed: %q", got)
	}
	// Expunge both, then a third fits under the limit again.
	ms.SetQuotaLimitFunc(func(string) int64 { return 1800 })
	c.cmd("q3 SELECT INBOX")
	c.cmd("q4 STORE 1:* +FLAGS (\\Deleted)")
	c.cmd("q5 EXPUNGE")
	if got := round75Tagged(round82Append(t, c, "q6", "INBOX", m2)); !strings.HasPrefix(got, "q6 OK") {
		t.Fatalf("DEFECT F5730: append after expunge still refused: %q", got)
	}
}

func TestRound91CopyQuota(t *testing.T) {
	c, ms := round82Server(t, round91Msg("one", 1000))
	// A copy shares the content-addressed blob: no new bytes, accepted at
	// a limit that exactly fits the original.
	ms.SetQuotaLimitFunc(func(string) int64 { return 1100 })
	c.cmd("c1 SELECT INBOX")
	if got := round75Tagged(c.cmd("c2 COPY 1 Archive")); !strings.HasPrefix(got, "c2 OK") {
		t.Fatalf("dedup copy refused: %q", got)
	}
	// Direct store-level check: over-quota copy error maps to ErrQuotaExceeded.
	_, err := ms.storeBlob("user@example.com", []byte(round91Msg("big", 5000)))
	if !errors.Is(err, storage.ErrQuotaExceeded) {
		t.Fatalf("DEFECT F5730: storeBlob over quota err=%v", err)
	}
}
