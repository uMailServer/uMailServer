package carddav

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// foldLines backed off to a cut of 0 on a run of UTF-8 continuation bytes
// and then looped forever, growing its buffer; a CardDAV client could store
// such a line and an edit from the webmail would exhaust the server's memory.
func TestFoldLinesInvalidUTF8Terminates(t *testing.T) {
	line := "X-JUNK:" + strings.Repeat("\x80", 200)
	done := make(chan string, 1)
	go func() { done <- foldLines([]string{line}) }()
	select {
	case out := <-done:
		if got := strings.ReplaceAll(out, "\r\n ", ""); got != line+"\r\n" {
			t.Fatalf("folding changed the content: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("foldLines did not return")
	}
}

// UpdateContact holds the storage lock across read and write, so a CardDAV
// write cannot land in between and be overwritten.
func TestUpdateContactIsAtomic(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateAddressbook("u@ex.com", &Addressbook{ID: "ab"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateContact("u@ex.com", "ab", "missing", func(o string) (string, error) { return o, nil }); !errors.Is(err, ErrContactNotFound) {
		t.Fatalf("missing contact: err=%v, want ErrContactNotFound", err)
	}
	if err := s.SaveContact("u@ex.com", "ab", &Contact{UID: "c"}, "v1"); err != nil {
		t.Fatal(err)
	}
	concurrent := make(chan struct{})
	err := s.UpdateContact("u@ex.com", "ab", "c", func(old string) (string, error) {
		go func() {
			_ = s.SaveContact("u@ex.com", "ab", &Contact{UID: "c"}, "carddav")
			close(concurrent)
		}()
		select {
		case <-concurrent:
			t.Error("a concurrent SaveContact ran inside UpdateContact")
		case <-time.After(100 * time.Millisecond):
		}
		return old + "+webmail", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-concurrent
	// The CardDAV write is ordered after the update, not lost inside it.
	if got, _ := s.GetContact("u@ex.com", "ab", "c"); got != "carddav" {
		t.Fatalf("contact = %q, want the later CardDAV write", got)
	}
}
