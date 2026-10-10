package store

import "testing"

func TestVerify5725Reproduction(t *testing.T) { TestAudit5725Failure(t) }

func TestVerify5725Edges(t *testing.T) {
	for in, want := range map[string]string{"": "", "S": "S", "SRS": "RS", "TDFSR": "DFRST", "RS": "RS"} {
		if got := canonicalFlags(in); got != want {
			t.Errorf("canonicalFlags(%q)=%q want %q", in, got, want)
		}
	}
	s := NewMaildirStore(t.TempDir())
	fn, _ := s.DeliverWithFlags("example.com", "u", "INBOX", []byte("m"), "SR")
	msgs, _ := s.List("example.com", "u", "INBOX")
	if len(msgs) != 1 || msgs[0].Flags != "RS" || fn != msgs[0].Filename {
		t.Fatalf("%q %+v", fn, msgs)
	}
	// Move preserves canonical flags
	if err := s.Move("example.com", "u", "INBOX", "Archive", fn); err != nil {
		t.Fatal(err)
	}
	am, _ := s.List("example.com", "u", "Archive")
	if len(am) != 1 || am[0].Flags != "RS" {
		t.Fatalf("%+v", am)
	}
}

func TestVerify5726Reproduction(t *testing.T) { TestAudit5726Failure(t) }

func TestVerify5726Edges(t *testing.T) {
	s := NewMaildirStore(t.TempDir())
	for _, c := range [][2]string{{"example.com", "."}, {".", "u"}, {"", "u"}, {"example.com", ".."}} {
		if _, err := s.Deliver(c[0], c[1], "INBOX", []byte("m")); err == nil {
			t.Errorf("%v accepted", c)
		}
	}
	if _, err := s.Deliver("example.co.uk", "first.last", "INBOX", []byte("m")); err != nil {
		t.Errorf("dotted names must stay valid: %v", err)
	}
}
