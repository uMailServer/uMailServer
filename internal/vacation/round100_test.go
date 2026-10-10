package vacation

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestF5822_ReplyDedupSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, nil)
	if err := m.SetConfig("u@example.com", &Config{Enabled: true, Subject: "s", Message: "m"}); err != nil {
		t.Fatal(err)
	}
	if !m.CheckAndRecordAutoReply("u@example.com", "Bob@b.com", nil) {
		t.Fatal("first reply should be allowed")
	}
	m2 := NewManager(dir, nil)
	if m2.CheckAndRecordAutoReply("u@example.com", "bob@b.com", nil) {
		t.Fatal("restart forgot the reply; sender answered again")
	}
	if !m2.CheckAndRecordAutoReply("u@example.com", "carol@b.com", nil) {
		t.Fatal("other sender should still be answered")
	}
	// new absence text resets records, including on disk
	if err := m2.SetConfig("u@example.com", &Config{Enabled: true, Subject: "s2", Message: "m"}); err != nil {
		t.Fatal(err)
	}
	m3 := NewManager(dir, nil)
	if !m3.CheckAndRecordAutoReply("u@example.com", "bob@b.com", nil) {
		t.Fatal("new vacation must reach previous senders")
	}
}

func TestF5822_ExpiredAndTornRecordsIgnored(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, nil)
	_ = m.SetConfig("u@example.com", &Config{Enabled: true, Subject: "s", Message: "m", SendInterval: time.Hour})
	old := time.Now().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	os.WriteFile(m.repliesPath("u@example.com"), []byte(`{"s":"old@b.com","t":"`+old+`"}`+"\n{\"s\":\"torn"), 0o600)
	m2 := NewManager(dir, nil)
	if !m2.CheckAndRecordAutoReply("u@example.com", "old@b.com", nil) {
		t.Fatal("expired record must not suppress")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) != 0 {
		t.Fatalf("temp files left: %v", m)
	}
}
