package vacation

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestR289Boundaries(t *testing.T) {
	loc := time.FixedZone("X", -9*3600-1800)
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, loc)
	end := time.Date(2026, 3, 8, 23, 59, 59, 0, loc)
	c := &Config{Enabled: true, StartDate: start, EndDate: end, SendInterval: time.Hour}
	chk := func(now time.Time, want bool) {
		_, ok := Eligible(c, "me@x.com", "a@b.com", nil, now)
		if ok != want {
			t.Errorf("now=%s got %v want %v", now, ok, want)
		}
	}
	chk(start.Add(-time.Nanosecond), false)
	chk(start, true)
	chk(end, true)
	chk(end.Add(time.Nanosecond), false)
	chk(end.UTC(), true) // same instant different zone
}

func TestR289Headers(t *testing.T) {
	c := &Config{Enabled: true, SendInterval: time.Hour}
	now := time.Now()
	for _, cfg := range []*Config{c, {Enabled: true, IgnoreLists: true, IgnoreBulk: true}} {
		cases := []struct {
			s string
			h map[string]string
		}{
			{"a@b.com", map[string]string{"Auto-Submitted": "auto-replied"}},
			{"a@b.com", map[string]string{"auto-submitted": "auto-generated"}},
			{"a@b.com", map[string]string{"Precedence": "Bulk"}},
			{"a@b.com", map[string]string{"Precedence": " list "}},
			{"a@b.com", map[string]string{"Precedence": "junk"}},
			{"a@b.com", map[string]string{"X-Auto-Response-Suppress": "All"}},
			{"", nil}, {"<>", nil}, {" <> ", nil},
			{"MAILER-DAEMON@b.com", nil}, {"<Mailer-Daemon>", nil},
			{"owner-x@b.com", nil}, {"x-request@b.com", nil},
			{"ME@X.com", nil},
		}
		for _, k := range cases {
			if _, ok := Eligible(cfg, "me@x.com", k.s, k.h, now); ok {
				t.Errorf("replied to %q %v", k.s, k.h)
			}
		}
	}
	// control
	if _, ok := Eligible(c, "me@x.com", "a@b.com", map[string]string{"Auto-Submitted": "no"}, now); !ok {
		t.Error("control failed")
	}
	// List-* with IgnoreLists false
	for _, hk := range []string{"List-Id", "List-Post", "List-Unsubscribe", "List-Help"} {
		_, ok := Eligible(c, "me@x.com", "a@b.com", map[string]string{hk: "<l.x>"}, now)
		t.Logf("IgnoreLists=false %s -> reply=%v", hk, ok)
	}
	for _, hk := range []string{"List-Id", "List-Post", "List-Unsubscribe", "List-Help", "List-Owner", "List-Subscribe", "List-Archive"} {
		_, ok := Eligible(&Config{Enabled: true, IgnoreLists: true}, "me@x.com", "a@b.com", map[string]string{hk: "<l.x>"}, now)
		t.Logf("IgnoreLists=true %s -> reply=%v", hk, ok)
	}
	for _, s := range []string{"noreply@b.com", "no-reply@b.com", "postmaster@b.com"} {
		t.Logf("Eligible %s -> %v ; IsAutomatedSender -> %v", s, func() bool { _, ok := Eligible(c, "me@x.com", s, nil, now); return ok }(), IsAutomatedSender(s))
	}
}

func TestR289ConcurrentAndGrowth(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := m.SetConfig("me@x.com", &Config{Enabled: true, Subject: "s", Message: "m", SendInterval: time.Hour}); err != nil {
		t.Fatal(err)
	}
	var n int32
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-gate
			s := "a@b.com"
			if i%2 == 0 {
				s = "<A@B.com>"
			}
			if m.CheckAndRecordAutoReply("me@x.com", s, nil) {
				atomic.AddInt32(&n, 1)
			}
		}(i)
	}
	close(gate)
	wg.Wait()
	if n != 1 {
		t.Fatalf("sent %d", n)
	}
	// growth: many distinct senders with expired interval -> bounded
	m.cacheMu.Lock()
	for i := 0; i < 5000; i++ {
		m.sentCache["me@x.com"]["old"+string(rune('a'+i%26))+strings.Repeat("x", i%7)+time.Duration(i).String()] = time.Now().Add(-2 * time.Hour)
	}
	m.cacheMu.Unlock()
	for i := 0; i < 3000; i++ {
		m.CheckAndRecordAutoReply("me@x.com", "n"+time.Duration(i).String()+"@b.com", nil)
	}
	m.cacheMu.RLock()
	sz := len(m.sentCache["me@x.com"])
	m.cacheMu.RUnlock()
	fi, _ := os.Stat(filepath.Join(dir, sanitizeFilename("me@x.com")+".replies"))
	t.Logf("cache size %d replies file %d bytes", sz, fi.Size())
	if sz > 4100 {
		t.Fatalf("expired entries retained: %d", sz)
	}
	// persisted exactly at interval boundary after restart
	m2 := NewManager(dir, nil)
	if m2.CheckAndRecordAutoReply("me@x.com", "a@b.com", nil) {
		t.Fatal("restart lost dedup")
	}
}
