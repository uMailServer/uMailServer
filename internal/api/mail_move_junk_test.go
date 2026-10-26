package api

// Regression test: webmail moves into and out of Junk now report the message
// to the spam classifier (it was never trained from any user action).

import (
	"net/http"
	"testing"
)

type junkReportCall struct {
	user, id string
	spam     bool
}

func TestMailMoveReportsJunk(t *testing.T) {
	h, mdb, ms := r143MailSetup(t)
	var calls []junkReportCall
	h.SetJunkReportFunc(func(user, id string, spam bool) {
		calls = append(calls, junkReportCall{user, id, spam})
	})
	user := "u@example.com"
	id := r143Deliver(t, mdb, ms, user, "INBOX", "offer", nil)
	for _, mb := range []string{"Junk", "Archive", "Trash"} {
		if err := mdb.CreateMailbox(user, mb); err != nil {
			t.Fatal(err)
		}
	}

	for _, step := range []struct{ from, to string }{
		{"inbox", "spam"},    // into Junk: spam
		{"spam", "Archive"},  // out of Junk: ham
		{"Archive", "Junk"},  // into Junk again: spam
		{"Junk", "trash"},    // Junk to Trash: no report
		{"trash", "Archive"}, // unrelated: no report
	} {
		if rec := r143Move(h, user, `{"id":"`+id+`","from":"`+step.from+`","to":"`+step.to+`"}`); rec.Code != http.StatusOK {
			t.Fatalf("move %s -> %s: %d %s", step.from, step.to, rec.Code, rec.Body.String())
		}
	}
	want := []junkReportCall{{user, id, true}, {user, id, false}, {user, id, true}}
	if len(calls) != len(want) {
		t.Fatalf("reports = %+v, want %+v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("report %d = %+v, want %+v", i, calls[i], want[i])
		}
	}
}
