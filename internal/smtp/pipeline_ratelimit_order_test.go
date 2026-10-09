package smtp

import (
	"net"
	"testing"

	"github.com/umailserver/umailserver/internal/ratelimit"
)

// F5128: a message refused for exceeding the recipient limit was counted
// against the user's persisted daily quota first.
func TestRateLimitStage_RecipientRefusalKeepsQuota(t *testing.T) {
	rl := ratelimit.New(nil, &ratelimit.Config{UserPerDay: 2, UserMaxRecipients: 1})
	defer rl.Stop()
	st := NewRateLimitStage(rl)
	msg := func(rcpts int) *MessageContext {
		to := make([]string, rcpts)
		for i := range to {
			to[i] = "r@x.org"
		}
		ctx := NewMessageContext(net.ParseIP("192.0.2.9"), "u@a.com", to, []byte("x"))
		ctx.Authenticated = true
		ctx.Username = "u@a.com"
		return ctx
	}
	for i := 0; i < 3; i++ {
		if r := st.Process(msg(2)); r != ResultReject {
			t.Fatalf("2-recipient message %d: got %d", i, r)
		}
	}
	for i := 0; i < 2; i++ {
		ctx := msg(1)
		if r := st.Process(ctx); r != ResultAccept {
			t.Fatalf("message %d within quota refused: %q", i, ctx.RejectionMessage)
		}
	}
	if r := st.Process(msg(1)); r != ResultReject {
		t.Fatalf("daily quota no longer enforced: got %d", r)
	}
}

// F5205: a message refused by the IP or global limit was still counted
// against the user's persisted daily quota.
func TestRateLimitStage_IPAndGlobalRefusalKeepQuota(t *testing.T) {
	for name, cfg := range map[string]*ratelimit.Config{
		"ip":     {UserPerDay: 10, IPPerMinute: 1},
		"global": {UserPerDay: 10, GlobalPerMinute: 1},
	} {
		t.Run(name, func(t *testing.T) {
			rl := ratelimit.New(nil, cfg)
			defer rl.Stop()
			st := NewRateLimitStage(rl)
			for i := 0; i < 3; i++ {
				ctx := NewMessageContext(net.ParseIP("192.0.2.9"), "u@a.com", []string{"r@x.org"}, []byte("x"))
				ctx.Authenticated = true
				ctx.Username = "u@a.com"
				want := ResultReject
				if i == 0 {
					want = ResultAccept
				}
				if r := st.Process(ctx); r != want {
					t.Fatalf("message %d: got %d (%q)", i, r, ctx.RejectionMessage)
				}
			}
			if got := rl.GetUserStats("u@a.com")["sent_today"]; got != int64(1) {
				t.Fatalf("sent_today = %v, want 1", got)
			}
		})
	}
}
