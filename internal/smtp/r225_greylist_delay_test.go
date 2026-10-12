package smtp

import (
	"net"
	"testing"
	"time"
)

func TestR225GreylistDelay(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	ctx := func() *MessageContext {
		return &MessageContext{From: "a@x.org", To: []string{"b@y.org"}, RemoteIP: net.ParseIP("192.0.2.1")}
	}
	s := NewGreylistStage(WithGreylistDelay(30*time.Second), WithGreylistClock(clock))
	if s.Process(ctx()) != ResultReject {
		t.Fatal("first attempt must be deferred")
	}
	now = now.Add(29 * time.Second)
	if s.Process(ctx()) != ResultReject {
		t.Fatal("retry before delay must be deferred")
	}
	now = now.Add(2 * time.Second)
	if s.Process(ctx()) == ResultReject {
		t.Fatal("retry after configured delay must pass")
	}

	d := NewGreylistStage(WithGreylistClock(clock), WithGreylistDelay(0))
	d.Process(ctx())
	now = now.Add(4 * time.Minute)
	if d.Process(ctx()) != ResultReject {
		t.Fatal("default delay is 5m")
	}
	now = now.Add(2 * time.Minute)
	if d.Process(ctx()) == ResultReject {
		t.Fatal("must pass after default delay")
	}
}
