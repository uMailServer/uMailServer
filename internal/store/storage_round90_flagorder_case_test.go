package store

import "testing"

func TestAudit5725Control(t *testing.T) {
	s := NewMaildirStore(t.TempDir())
	fn, _ := s.Deliver("example.com", "u", "INBOX", []byte("m"))
	if err := s.SetFlags("example.com", "u", "INBOX", fn, "RS"); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	msgs, _ := s.List("example.com", "u", "INBOX")
	if len(msgs) != 1 || msgs[0].Flags != "RS" {
		t.Fatalf("INVALID control: %+v", msgs)
	}
}

// FAILURE: Maildir requires the info flags after ":2," in ASCII order without
// duplicates; caller order is written verbatim.
func TestAudit5725Failure(t *testing.T) {
	s := NewMaildirStore(t.TempDir())
	fn, _ := s.Deliver("example.com", "u", "INBOX", []byte("m"))
	if err := s.SetFlags("example.com", "u", "INBOX", fn, "SRS"); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	msgs, _ := s.List("example.com", "u", "INBOX")
	t.Logf("EXPECTED: flags \"RS\" ACTUAL: %q", msgs[0].Flags)
	if msgs[0].Flags != "RS" {
		t.Fatalf("DEFECT F5725: flags %q not canonical", msgs[0].Flags)
	}
}
