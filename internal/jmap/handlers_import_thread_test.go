package jmap

import "testing"

const r295JUser = "thr-user@example.com"

func r295JImport(t *testing.T, s *Server, store interface {
	StoreMessage(string, []byte) (string, error)
}, raw string) Email {
	t.Helper()
	blob, err := store.StoreMessage(r295JUser, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp := s.handleEmailImport(r295JUser, MethodCall{Name: "Email/import", ID: "c", Args: map[string]interface{}{
		"accountId": r295JUser,
		"emails":    map[string]interface{}{"i": map[string]interface{}{"blobId": blob, "mailboxIds": map[string]interface{}{"inbox": true}}},
	}})
	created, _ := resp.Args["created"].(map[string]Email)
	e, ok := created["i"]
	if !ok {
		t.Fatalf("import failed: %v", resp.Args)
	}
	return e
}

// F7690: imported messages get a thread id; a reply shares its parent's, and
// unrelated messages do not share one.
func TestEmailImportAssignsThreadIDs(t *testing.T) {
	server, db, ms, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(r295JUser, "INBOX"); err != nil {
		t.Fatal(err)
	}
	root := r295JImport(t, server, ms, "From: a@x.test\r\nTo: b@y.test\r\nSubject: Thread root\r\nMessage-ID: <root1@x.test>\r\n\r\nroot\r\n")
	reply := r295JImport(t, server, ms, "From: b@y.test\r\nTo: a@x.test\r\nSubject: Re: Thread root\r\nMessage-ID: <reply1@y.test>\r\nIn-Reply-To: <root1@x.test>\r\nReferences: <root1@x.test>\r\n\r\nreply\r\n")
	other := r295JImport(t, server, ms, "From: c@z.test\r\nTo: a@x.test\r\nSubject: Something else entirely\r\nMessage-ID: <other1@z.test>\r\n\r\nother\r\n")
	if root.ThreadID == "" {
		t.Fatal("imported message has no thread id")
	}
	if reply.ThreadID != root.ThreadID {
		t.Fatalf("reply thread %q != root thread %q", reply.ThreadID, root.ThreadID)
	}
	if other.ThreadID == "" || other.ThreadID == root.ThreadID {
		t.Fatalf("unrelated message thread %q (root %q)", other.ThreadID, root.ThreadID)
	}
}

// The imported conversation also appears in the thread summary the REST thread
// list reads (F7690 review follow-up): the import used to set a threadId but
// never wrote the summary row, so /api/v1/threads did not list it.
func TestEmailImportRecordsThreadSummary(t *testing.T) {
	server, db, ms, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(r295JUser, "INBOX"); err != nil {
		t.Fatal(err)
	}
	root := r295JImport(t, server, ms, "From: a@x.test\r\nTo: b@y.test\r\nSubject: Plan\r\nMessage-ID: <s1@x.test>\r\n\r\nroot\r\n")
	r295JImport(t, server, ms, "From: b@y.test\r\nTo: a@x.test\r\nSubject: Re: Plan\r\nMessage-ID: <s2@y.test>\r\nIn-Reply-To: <s1@x.test>\r\nReferences: <s1@x.test>\r\n\r\nreply\r\n")
	th, err := db.GetThread(r295JUser, root.ThreadID)
	if err != nil {
		t.Fatalf("no thread summary for %s: %v", root.ThreadID, err)
	}
	if th.MessageCount != 2 || len(th.Participants) != 2 {
		t.Fatalf("summary = %+v, want 2 messages and 2 participants", th)
	}
}
