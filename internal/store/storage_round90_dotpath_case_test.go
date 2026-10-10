package store

import "testing"

func TestAudit5726Control(t *testing.T) {
	s := NewMaildirStore(t.TempDir())
	if _, err := s.Deliver("example.com", "u", "INBOX", []byte("m")); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	if _, err := s.Deliver("example.com", "..", "INBOX", []byte("m")); err == nil {
		t.Fatalf("INVALID: .. must be rejected")
	}
}

// FAILURE: user "." names the "users" directory's child "Maildir", i.e. the
// parent of user "Maildir"'s Maildir; domain "." collapses a path level.
func TestAudit5726Failure(t *testing.T) {
	s := NewMaildirStore(t.TempDir())
	_, e1 := s.userMaildirPath("example.com", ".")
	_, e2 := s.userMaildirPath(".", "u")
	t.Logf("EXPECTED: both rejected ACTUAL: user=%v domain=%v", e1, e2)
	if e1 == nil || e2 == nil {
		t.Fatalf("DEFECT F5726: \".\" accepted as path component (user err=%v, domain err=%v)", e1, e2)
	}
}
