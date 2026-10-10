package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestVerify5720Reproduction(t *testing.T) { TestAudit5720Failure(t) }

func TestVerify5720Edges(t *testing.T) {
	ms, _ := NewMessageStore(t.TempDir())
	a := []byte(fmt.Sprintf("%0100d", 1))
	b := []byte(fmt.Sprintf("%0100d", 2))
	c := []byte(fmt.Sprintf("%0100d", 3))
	if _, err := ms.StoreMessageWithQuota("u", a, 200); err != nil {
		t.Fatal(err)
	}
	// exact boundary accepted
	idb, err := ms.StoreMessageWithQuota("u", b, 200)
	if err != nil {
		t.Fatalf("boundary: %v", err)
	}
	// one past rejected with typed error and no side effect
	_, err = ms.StoreMessageWithQuota("u", c, 200)
	var qe *QuotaExceededError
	if !errors.Is(err, ErrQuotaExceeded) || !errors.As(err, &qe) || qe.Used != 200 || qe.Limit != 200 {
		t.Fatalf("want typed quota error, got %v", err)
	}
	if u, _ := ms.UserUsage("u"); u != 200 {
		t.Fatalf("usage %d", u)
	}
	// dedup of existing content at full quota is free
	if _, err := ms.StoreMessageWithQuota("u", a, 200); err != nil {
		t.Fatalf("dedup: %v", err)
	}
	// delete frees space (recount, no stale counter)
	if err := ms.DeleteMessage("u", idb); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.StoreMessageWithQuota("u", c, 200); err != nil {
		t.Fatalf("after delete: %v", err)
	}
	// unlimited and per-user isolation
	if _, err := ms.StoreMessageWithQuota("u", b, 0); err != nil {
		t.Fatal(err)
	}
	if u, _ := ms.UserUsage("nobody"); u != 0 {
		t.Fatalf("empty user usage %d", u)
	}
	if _, err := ms.UserUsage("."); err == nil {
		t.Fatal("dot user")
	}
}

// Gated: all goroutines are released together by closing a barrier; exactly
// limit/size stores may succeed.
func TestVerify5720ConcurrentDeliveries(t *testing.T) {
	ms, _ := NewMessageStore(t.TempDir())
	const n, size, fit = 24, 100, 5
	start := make(chan struct{})
	var ok, over atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := ms.StoreMessageWithQuota("u", []byte(fmt.Sprintf("%0100d", i)), size*fit)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrQuotaExceeded):
				over.Add(1)
			default:
				t.Errorf("unexpected %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if ok.Load() != fit || over.Load() != n-fit {
		t.Fatalf("ok=%d over=%d", ok.Load(), over.Load())
	}
	if u, _ := ms.UserUsage("u"); u != size*fit {
		t.Fatalf("usage %d", u)
	}
}

func TestVerify5721Reproduction(t *testing.T) { TestAudit5721Failure(t) }

func TestVerify5721Edges(t *testing.T) {
	ms, _ := NewMessageStore(t.TempDir())
	for _, u := range []string{".", "..", "", "a/b"} {
		if _, err := ms.StoreMessage(u, []byte("hello world body")); err == nil {
			t.Errorf("%q accepted", u)
		}
		if _, err := ms.ReadMessage(u, "abcdef"); err == nil {
			t.Errorf("read %q accepted", u)
		}
		if ms.MessageExists(u, "abcdef") || ms.DeleteMessage(u, "abcdef") == nil {
			t.Errorf("exists/delete %q", u)
		}
	}
	if _, err := ms.StoreMessage("a.b", []byte("hello world body")); err != nil {
		t.Errorf("dotted name must stay valid: %v", err)
	}
}

func TestVerify5722Reproduction(t *testing.T) { TestAudit5722Failure(t) }

func TestVerify5722Edges(t *testing.T) {
	db := audit5722DB(t)
	m1, _ := db.GetNextModSeq("u", "X")
	_ = db.CreateMailbox("u", "X")
	m2, _ := db.GetNextModSeq("u", "X")
	if m2 != m1+1 {
		t.Fatalf("modseq reissued: %d then %d", m1, m2)
	}
	mb, _ := db.GetMailbox("u", "X")
	if mb.UIDValidity == 0 {
		t.Fatal("uidvalidity not set by CreateMailbox")
	}
	// repeated CreateMailbox keeps counters and validity
	_, _ = db.GetNextUID("u", "X")
	_ = db.CreateMailbox("u", "X")
	mb2, _ := db.GetMailbox("u", "X")
	if mb2.UIDValidity != mb.UIDValidity || mb2.UIDNext != 2 {
		t.Fatalf("%+v vs %+v", mb, mb2)
	}
}

func TestVerify5723Reproduction(t *testing.T) { TestAudit5723Failure(t) }

func TestVerify5723Edges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	db, _ := OpenDatabase(path)
	_ = db.CreateMailbox("u", "A")
	a, _ := db.GetMailbox("u", "A")
	_ = db.CreateMailbox("u", "Other")
	// rename away then recreate the old name
	if err := db.RenameMailbox("u", "A", "B"); err != nil {
		t.Fatal(err)
	}
	_ = db.CreateMailbox("u", "A")
	a2, _ := db.GetMailbox("u", "A")
	if a2.UIDValidity <= a.UIDValidity {
		t.Fatalf("rename-away reuse %d <= %d", a2.UIDValidity, a.UIDValidity)
	}
	// floor survives reopen
	_ = db.DeleteMailbox("u", "A")
	_ = db.Close()
	db, _ = OpenDatabase(path)
	defer db.Close()
	_ = db.CreateMailbox("u", "A")
	a3, _ := db.GetMailbox("u", "A")
	if a3.UIDValidity <= a2.UIDValidity {
		t.Fatalf("after reopen %d <= %d", a3.UIDValidity, a2.UIDValidity)
	}
	// mailbox listing is not polluted by the floor bucket
	ls, _ := db.ListMailboxes("u")
	for _, n := range ls {
		if n == "uidvalidity_floor" {
			t.Fatal("floor bucket leaked into listing")
		}
	}
}
