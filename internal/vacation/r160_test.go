package vacation

import (
	"os"
	"strings"
	"testing"
	"time"
)

// F6415: CR/LF in the subject would inject headers into the auto-reply.
func TestF6415_SubjectHeaderInjectionRejected(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	for _, subj := range []string{"Away\r\nBcc: x@evil.com", "Away\nX: y", "Away\rX"} {
		if err := m.SetConfig("me@a.com", &Config{Enabled: true, Subject: subj, Message: "m"}); err == nil {
			t.Errorf("subject %q accepted", subj)
		}
	}
}

// F6416: header names are case-insensitive; callers may pass lower-case keys.
func TestF6416_HeaderLookupCaseInsensitive(t *testing.T) {
	m := newAwayManager(t, time.Hour)
	for k, v := range map[string]string{
		"auto-submitted": "auto-replied", "precedence": "bulk",
		"list-id": "<x.y>", "x-auto-response-suppress": "All",
	} {
		if m.CheckAndRecordAutoReply("me@a.com", "s"+k+"@b.com", map[string]string{k: v}) {
			t.Errorf("replied despite %s", k)
		}
	}
}

// F6417: a non-positive interval (hand-edited config) must not mean
// "reply to every message".
func TestF6417_ZeroIntervalStillDedups(t *testing.T) {
	m := newAwayManager(t, time.Hour)
	m.mu.Lock()
	m.configs["me@a.com"].SendInterval = 0
	m.mu.Unlock()
	if !m.CheckAndRecordAutoReply("me@a.com", "b@b.com", nil) {
		t.Fatal("first refused")
	}
	if m.CheckAndRecordAutoReply("me@a.com", "b@b.com", nil) {
		t.Fatal("second reply with zero interval")
	}
}

// F6418: the persisted reply log is compacted when the in-memory store is
// pruned, so it does not grow without bound.
func TestF6418_ReplyLogBounded(t *testing.T) {
	m := newAwayManager(t, time.Hour)
	old := time.Now().Add(-2 * time.Hour)
	m.cacheMu.Lock()
	m.sentCache["me@a.com"] = map[string]time.Time{}
	for i := 0; i < minPruneSize+5; i++ {
		m.sentCache["me@a.com"]["x"+strings.Repeat("a", i%50)+string(rune('a'+i%26))+time.Duration(i).String()+"@b.com"] = old
	}
	m.cacheMu.Unlock()
	// Pre-bloat the file as if many replies had been appended earlier.
	bloat := strings.Repeat(`{"s":"old@b.com","t":"2020-01-01T00:00:00Z"}`+"\n", 5000)
	if err := os.WriteFile(m.repliesPath("me@a.com"), []byte(bloat), 0o600); err != nil {
		t.Fatal(err)
	}
	if !m.CheckAndRecordAutoReply("me@a.com", "new@b.com", nil) {
		t.Fatal("refused")
	}
	data, _ := os.ReadFile(m.repliesPath("me@a.com"))
	if n := strings.Count(string(data), "\n"); n > 10 {
		t.Fatalf("reply log has %d lines after prune", n)
	}
}
