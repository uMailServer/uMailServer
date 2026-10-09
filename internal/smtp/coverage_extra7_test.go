package smtp

import (
	"net"
	"testing"

	"github.com/umailserver/umailserver/internal/sieve"
)

// These replaced the extractUserFromRecipient tests: the helper keyed
// scripts by the recipient's local part, which selected another domain's
// user's script (F5125). The stage no longer runs scripts at all; the
// delivery handler runs each recipient's script.

func TestSieveStageAcceptsWhateverTheScriptSays(t *testing.T) {
	m := sieve.NewManager()
	for _, k := range []string{"user@example.com", "user"} {
		if err := m.SetActiveScript(k, "s", "require \"reject\";\nreject \"no\";\n"); err != nil {
			t.Fatalf("SetActiveScript(%q): %v", k, err)
		}
	}
	st := NewSieveStage(m)
	for _, rcpt := range []string{"user@example.com", "user@other.example", "", "username", "user!otherdomain!mailbox"} {
		ctx := NewMessageContext(net.ParseIP("192.0.2.1"), "s@x.org", []string{rcpt}, []byte("Subject: x\r\n\r\nb\r\n"))
		if r := st.Process(ctx); r != ResultAccept || ctx.Rejected {
			t.Errorf("%q: got %d rejected=%v", rcpt, r, ctx.Rejected)
		}
	}
}
