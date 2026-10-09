package vacation

import (
	"fmt"
	"testing"
	"time"
)

func newAwayManager(t *testing.T, interval time.Duration) *Manager {
	t.Helper()
	m := NewManager(t.TempDir(), nil)
	if err := m.SetConfig("me@a.com", &Config{Enabled: true, Subject: "Away", Message: "Away", SendInterval: interval, IgnoreLists: true, IgnoreBulk: true}); err != nil {
		t.Fatal(err)
	}
	return m
}

// F5206: RFC 3834 §2 — no reply to a null return path, MAILER-DAEMON,
// owner-* or *-request, and Precedence values match case-insensitively.
func TestAutoReply_RFC3834Suppression(t *testing.T) {
	m := newAwayManager(t, time.Hour)
	for _, s := range []string{"", "<>", "MAILER-DAEMON@b.com", "owner-list@b.com", "list-request@b.com"} {
		if m.CheckAndRecordAutoReply("me@a.com", s, nil) {
			t.Errorf("replied to sender %q", s)
		}
	}
	for _, p := range []string{"Bulk", "LIST", "Junk"} {
		if m.CheckAndRecordAutoReply("me@a.com", "p"+p+"@b.com", map[string]string{"Precedence": p}) {
			t.Errorf("replied to Precedence %s", p)
		}
	}
	if !m.CheckAndRecordAutoReply("me@a.com", "requester@b.com", nil) {
		t.Error("ordinary sender refused")
	}
}

// F5207: sender addresses compare case-insensitively for dedup and exclusion.
func TestAutoReply_SenderCaseInsensitive(t *testing.T) {
	m := newAwayManager(t, time.Hour)
	if !m.CheckAndRecordAutoReply("me@a.com", "Bob@B.com", nil) {
		t.Fatal("first reply refused")
	}
	if m.CheckAndRecordAutoReply("me@a.com", "bob@b.com", nil) {
		t.Error("second reply to the same sender within the interval")
	}
	cfg, _ := m.GetConfig("me@a.com")
	cfg.ExcludeAddresses = []string{"boss@b.com"}
	if err := m.SetConfig("me@a.com", cfg); err != nil {
		t.Fatal(err)
	}
	if m.CheckAndRecordAutoReply("me@a.com", "Boss@B.com", nil) {
		t.Error("excluded address answered")
	}
}

// F5208: expired dedup records are pruned, records within the interval kept.
func TestAutoReply_DedupStoreBounded(t *testing.T) {
	m := newAwayManager(t, time.Hour)
	old := time.Now().Add(-2 * time.Hour)
	m.cacheMu.Lock()
	m.sentCache["me@a.com"] = map[string]time.Time{}
	for i := 0; i < 4*minPruneSize; i++ {
		m.sentCache["me@a.com"][fmt.Sprintf("old%d@b.com", i)] = old
	}
	m.cacheMu.Unlock()
	m.CheckAndRecordAutoReply("me@a.com", "new@b.com", nil)
	m.cacheMu.RLock()
	n := len(m.sentCache["me@a.com"])
	m.cacheMu.RUnlock()
	if n != 1 {
		t.Fatalf("records after prune = %d, want 1", n)
	}
	if m.CheckAndRecordAutoReply("me@a.com", "new@b.com", nil) {
		t.Error("record within the interval was pruned")
	}
}

// F5209: a new vacation (re-enabled or changed text) answers senders that
// were answered about the previous one; re-saving the same one does not.
func TestAutoReply_NewVacationResetsDedup(t *testing.T) {
	m := newAwayManager(t, time.Hour)
	m.CheckAndRecordAutoReply("me@a.com", "bob@b.com", nil)
	cfg, _ := m.GetConfig("me@a.com")
	if err := m.SetConfig("me@a.com", cfg); err != nil {
		t.Fatal(err)
	}
	if m.CheckAndRecordAutoReply("me@a.com", "bob@b.com", nil) {
		t.Fatal("re-saving the same vacation reset dedup")
	}
	cfg.Message = "At a conference until Friday"
	if err := m.SetConfig("me@a.com", cfg); err != nil {
		t.Fatal(err)
	}
	if !m.CheckAndRecordAutoReply("me@a.com", "bob@b.com", nil) {
		t.Error("sender not told about the new vacation")
	}
}
