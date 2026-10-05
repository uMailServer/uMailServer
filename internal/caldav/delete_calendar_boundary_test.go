package caldav

import (
	"testing"
)

func TestAuditRegressionF17(t *testing.T) {
	fixture := func() *Storage {
		s := NewStorage(t.TempDir())
		for _, u := range []string{"alice", "bob"} {
			if e := s.CreateCalendar(u, &Calendar{ID: "personal", Name: u}); e != nil {
				t.Fatal(e)
			}
		}
		return s
	}
	control := fixture()
	if e := control.DeleteCalendar("alice", "personal"); e != nil {
		t.Fatal(e)
	}
	a, e := control.GetCalendar("alice", "personal")
	if e != nil {
		t.Fatal(e)
	}
	b, e := control.GetCalendar("bob", "personal")
	if e != nil {
		t.Fatal(e)
	}
	if a != nil || b == nil {
		t.Fatal("invalid control")
	}
	failed := false
	for _, id := range []string{"../bob/personal", ".", ""} {
		s := fixture()
		victim := "alice"
		if id == "../bob/personal" {
			victim = "bob"
		}
		e := s.DeleteCalendar("alice", id)
		v, readErr := s.GetCalendar(victim, "personal")
		if readErr != nil {
			t.Fatal(readErr)
		}
		if e == nil || v == nil {
			failed = true
		}
	}
	if failed {
		t.Fatal("invalid calendar ID removed an unrelated calendar")
	}
	s := fixture()
	for i := 0; i < 2; i++ {
		if e := s.DeleteCalendar("alice", "missing-calendar"); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.DeleteCalendar("alice", "personal"); e != nil {
		t.Fatal(e)
	}
	if e := s.DeleteCalendar("alice", "personal"); e != nil {
		t.Fatal("repeat deletion", e)
	}
	if b, e := s.GetCalendar("bob", "personal"); e != nil || b == nil {
		t.Fatal("normal deletion changed other user")
	}
}
