package server

// Regression tests for F5118: a message delivered to a folder other than
// INBOX (Junk, a Sieve fileinto target) is indexed for search under its own
// folder, not as the INBOX message with the same UID.

import (
	"testing"

	"github.com/umailserver/umailserver/internal/search"
)

func indexFolderSearch(t *testing.T, srv *Server, q string) []search.MessageSearchResult {
	t.Helper()
	res, err := srv.searchSvc.Search(search.MessageSearchOptions{User: "alice@test.com", Query: q})
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	return res
}

func indexFolderMsg(subject, body string) []byte {
	return []byte("Subject: " + subject + "\r\nDate: Mon, 1 Jan 2024 00:00:00 +0000\r\nMessage-ID: <" + subject + "@x>\r\n\r\n" + body + "\r\n")
}

func TestIndexJobUsesDeliveryFolder(t *testing.T) {
	srv := junkRTServer(t, true)
	if srv.searchSvc == nil {
		t.Fatal("no search service")
	}
	indexFolderSearch(t, srv, "warmup") // builds alice's empty index
	srv.wg.Add(1)
	go srv.runIndexWorker()

	sieveDTScript(t, srv, "alice@test.com", "require \"fileinto\";\nif header :contains \"Subject\" \"report\" { fileinto \"Work\"; }\n")
	for _, d := range []struct {
		subject, body, folder string
	}{
		{"fruit", "apple pie", ""},
		{"animal", "zebra stripes", "Junk"},
		{"report", "quarterly numbers", ""}, // filed into Work by Sieve
	} {
		if err := srv.deliverMessageToFolder("bob@test.com", []string{"alice@test.com"}, nil, indexFolderMsg(d.subject, d.body), d.folder); err != nil {
			t.Fatalf("deliver %s: %v", d.subject, err)
		}
	}
	_ = srv.Stop() // closes indexWork and waits for the worker

	for q, folder := range map[string]string{"apple": "INBOX", "zebra": "Junk", "quarterly": "Work"} {
		res := indexFolderSearch(t, srv, q)
		if len(res) != 1 || res[0].Folder != folder || res[0].UID != 1 {
			t.Fatalf("F5118: search %q = %+v, want one hit in %s UID 1", q, res, folder)
		}
	}
}
