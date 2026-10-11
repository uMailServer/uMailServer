package push

import (
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// F6967: pushes in flight are bounded no matter how many users/mails trigger
// them; excess pushes are dropped instead of spawning unbounded requests.
func TestSendToUserBoundsInFlightSends(t *testing.T) {
	s := &Service{subscriptions: map[string]*Subscription{}, userSubs: map[string][]string{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	const users = 8
	for u := 0; u < users; u++ {
		uid := string(rune('a' + u))
		for i := 0; i < MaxSubscriptionsPerUser; i++ {
			id := uid + strings.Repeat("x", i+1)
			s.subscriptions[id] = &Subscription{ID: id, UserID: uid, Endpoint: "https://push.example/" + id}
			s.userSubs[uid] = append(s.userSubs[uid], id)
		}
	}
	var inFlight, peak atomic.Int64
	started := make(chan struct{}, users*MaxSubscriptionsPerUser)
	release := make(chan struct{})
	s.sendFn = func(*Subscription, *Notification) error {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		inFlight.Add(-1)
		return nil
	}

	var wg sync.WaitGroup
	for u := 0; u < users; u++ {
		wg.Add(1)
		go func(uid string) {
			defer wg.Done()
			_ = s.SendToUser(uid, &Notification{Title: "t"})
		}(string(rune('a' + u)))
	}
	// Gate on events, not time: every send either holds a slot (started) or
	// is counted as dropped.
	for i := 0; i < MaxConcurrentSends; i++ {
		<-started
	}
	deadline := time.Now().Add(20 * time.Second)
	for s.DroppedPushes() < uint64(users*MaxSubscriptionsPerUser-MaxConcurrentSends) {
		if time.Now().After(deadline) {
			t.Fatalf("dropped=%d, want %d", s.DroppedPushes(), users*MaxSubscriptionsPerUser-MaxConcurrentSends)
		}
		runtime.Gosched()
	}
	close(release)
	wg.Wait()
	if p := peak.Load(); p > MaxConcurrentSends {
		t.Fatalf("peak in-flight sends = %d, want <= %d", p, MaxConcurrentSends)
	}
}

func TestSubscribeRejectsOversizedFields(t *testing.T) {
	s, err := NewService(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a", 5000)
	if err := s.Subscribe("u", &Subscription{Endpoint: "https://push.example/" + long, P256dh: "k", Auth: "a"}); err == nil {
		t.Fatal("oversized endpoint accepted")
	}
	if err := s.Subscribe("u", &Subscription{Endpoint: "https://push.example/x", P256dh: long, Auth: "a"}); err == nil {
		t.Fatal("oversized key accepted")
	}
	if err := s.Subscribe("u", &Subscription{Endpoint: "https://push.example/x", DeviceInfo: DeviceInfo{Name: long}}); err == nil {
		t.Fatal("oversized device name accepted")
	}
	sub := &Subscription{Endpoint: "https://push.example/ok", P256dh: "k", Auth: "a"}
	if err := s.Subscribe("u", sub); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDeviceInfo("u", sub.ID, DeviceInfo{Name: long}); err == nil {
		t.Fatal("oversized device info accepted by UpdateDeviceInfo")
	}
}
