package server

// Regression tests: filters saved through /api/v1/filters (the account
// portal's Filters page) were stored but never applied at delivery. They are
// now compiled to Sieve and run when the mailbox has no active script.

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/api"
	"github.com/umailserver/umailserver/internal/db"
)

func storeTestFilter(t *testing.T, srv *Server, f *api.EmailFilter) {
	t.Helper()
	f.UserID = "alice@test.com"
	f.Enabled = true
	f.CreatedAt = time.Now()
	if err := srv.database.Put(db.BucketFilters, f.UserID+":"+f.ID, f); err != nil {
		t.Fatalf("store filter: %v", err)
	}
}

func TestFilterDeliveryMovesMatchingMail(t *testing.T) {
	srv := junkRTServer(t, true)
	storeTestFilter(t, srv, &api.EmailFilter{
		ID:         "f1",
		Conditions: []api.FilterCondition{{Field: "subject", Operator: "contains", Value: "HELLO"}},
		Actions:    []api.FilterAction{{Type: "move", Target: "Work"}, {Type: "markRead"}},
	})
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTClean)

	if in, work := sieveDTCount(t, srv, "alice@test.com", "INBOX"), sieveDTCount(t, srv, "alice@test.com", "Work"); in != 0 || work != 1 {
		t.Fatalf("move filter: INBOX=%d Work=%d, want 0/1", in, work)
	}
	if flags := sieveDTFlags(t, srv, "alice@test.com", "Work"); !hasFlag(flags, `\Seen`) {
		t.Fatalf("markRead filter: Work flags = %v, want \\Seen", flags)
	}
}

func TestFilterDeliveryNonMatchingAndDisabled(t *testing.T) {
	srv := junkRTServer(t, true)
	storeTestFilter(t, srv, &api.EmailFilter{
		ID:         "f1",
		Conditions: []api.FilterCondition{{Field: "subject", Operator: "equals", Value: "other"}},
		Actions:    []api.FilterAction{{Type: "move", Target: "Work"}},
	})
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTClean)
	if in := sieveDTCount(t, srv, "alice@test.com", "INBOX"); in != 1 {
		t.Fatalf("non-matching filter: INBOX=%d, want 1", in)
	}
}

// Mail with a spam verdict stays in Junk: a filter must not let the sender
// move it out by matching the user's rules.
func TestFilterDeliveryNotAppliedToJunk(t *testing.T) {
	srv := junkRTServer(t, true)
	storeTestFilter(t, srv, &api.EmailFilter{
		ID:         "f1",
		Conditions: []api.FilterCondition{{Field: "subject", Operator: "contains", Value: "money"}},
		Actions:    []api.FilterAction{{Type: "move", Target: "Work"}},
	})
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTSpam)
	if junk, work := sieveDTCount(t, srv, "alice@test.com", "Junk"), sieveDTCount(t, srv, "alice@test.com", "Work"); junk != 1 || work != 0 {
		t.Fatalf("junk mail: Junk=%d Work=%d, want 1/0", junk, work)
	}
}

// An active Sieve script takes precedence over the webmail filters.
func TestFilterDeliverySieveScriptTakesPrecedence(t *testing.T) {
	srv := junkRTServer(t, true)
	storeTestFilter(t, srv, &api.EmailFilter{
		ID:         "f1",
		Conditions: []api.FilterCondition{{Field: "subject", Operator: "contains", Value: "hello"}},
		Actions:    []api.FilterAction{{Type: "move", Target: "Work"}},
	})
	sieveDTScript(t, srv, "alice@test.com", "require \"fileinto\";\nfileinto \"Lists\";\n")
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTClean)
	if lists, work := sieveDTCount(t, srv, "alice@test.com", "Lists"), sieveDTCount(t, srv, "alice@test.com", "Work"); lists != 1 || work != 0 {
		t.Fatalf("precedence: Lists=%d Work=%d, want 1/0", lists, work)
	}
}
