package server

import (
	"strings"
	"testing"
)

const r229Msg = "From: bob@example.org\r\nTo: alice@test.com\r\nSubject: hi\r\n\r\nbody\r\n"

// F7060: a metadata write failure after the blob is stored must release the
// reserved quota and remove the unreferenced blob, and tell the sender.
func TestR229_MetadataFailureReleasesQuotaAndBlob(t *testing.T) {
	srv := junkRTServer(t, true)
	if err := srv.storageDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	err := srv.deliverLocal("alice", "test.com", "bob@example.org", []byte(r229Msg))
	if err == nil {
		t.Fatalf("expected delivery error when metadata cannot be written")
	}
	acc, _ := srv.database.GetAccount("test.com", "alice")
	if acc.QuotaUsed != 0 {
		t.Fatalf("quota leaked: %d", acc.QuotaUsed)
	}
	data := []byte(r229Msg)
	_ = data
	n, uerr := srv.msgStore.UserUsage("alice@test.com")
	if uerr != nil || n != 0 {
		t.Fatalf("blob leaked: usage=%d err=%v", n, uerr)
	}
}

// F7061: relayMessage without a queue must fail, not report success.
func TestR229_RelayNilQueueErrors(t *testing.T) {
	srv := junkRTServer(t, true)
	srv.queue = nil
	if err := srv.relayMessage("a@test.com", "b@example.org", []byte(r229Msg)); err == nil {
		t.Fatalf("expected error with nil queue")
	}
}

// F7062: Junk-verdict mail is kept locally and not auto-forwarded.
func TestR229_JunkNotForwarded(t *testing.T) {
	srv := junkRTServer(t, true)
	acc, _ := srv.database.GetAccount("test.com", "alice")
	acc.ForwardTo = "carol@example.net"
	if err := srv.database.UpdateAccount(acc); err != nil {
		t.Fatalf("update: %v", err)
	}
	spam := "X-Spam-Status: Yes\r\n" + r229Msg
	if err := srv.deliverMessageToFolder("bob@example.org", []string{"alice@test.com"}, nil, []byte(spam), "Junk"); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if _, junk := junkRTCounts(t, srv); junk != 1 {
		t.Fatalf("junk copy missing: %d", junk)
	}
	entries, _ := srv.queue.GetPendingEntries()
	for _, e := range entries {
		if strings.Contains(strings.Join(e.To, ","), "carol@example.net") {
			t.Fatalf("junk mail was forwarded")
		}
	}
	// control: ham is forwarded
	if err := srv.deliverMessageToFolder("bob@example.org", []string{"alice@test.com"}, nil, []byte(r229Msg), ""); err != nil {
		t.Fatalf("deliver ham: %v", err)
	}
	entries, _ = srv.queue.GetPendingEntries()
	found := false
	for _, e := range entries {
		if strings.Contains(strings.Join(e.To, ","), "carol@example.net") {
			found = true
		}
	}
	if !found {
		t.Fatalf("ham not forwarded")
	}
}
