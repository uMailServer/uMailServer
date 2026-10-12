package jmap

// R235 regression tests (F7120-F7123).
//
// F7120: accountNotFound / method-level serverFail must be an "error" method
//        response (RFC 8620 §3.6.2), not a response named after the method.
// F7121: requests over the advertised maxSizeRequest are rejected with a
//        400 urn:ietf:params:jmap:error:limit / maxSizeRequest (RFC 8620 §3.6.1).
// F7122: Mailbox/set destroy with onDestroyRemoveEmails must delete the
//        removed emails reference-aware (journal, blob, quota), like Email/set.
// F7123: Email/import must hold the per-user reference guard so a concurrent
//        Email/destroy cannot release the blob it is about to reference.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

func TestR235AccountNotFoundIsErrorResponse(t *testing.T) {
	srv, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	const user = "alice@example.com"
	_ = db.CreateMailbox(user, "INBOX")

	// Control: own account answers under the method name.
	ok := srv.dispatchMethodCall(user, MethodCall{Name: "Email/changes", ID: "c", Args: map[string]interface{}{"accountId": user, "sinceState": "0"}})
	if ok.Name != "Email/changes" {
		t.Fatalf("CONTROL FAILED: own-account response name = %q", ok.Name)
	}
	for _, m := range []string{"Mailbox/get", "Email/get", "Email/set", "Email/changes", "Email/query", "Thread/get", "Identity/get", "Mailbox/queryChanges"} {
		resp := srv.dispatchMethodCall(user, MethodCall{Name: m, ID: "c", Args: map[string]interface{}{"accountId": "bob@example.com", "sinceState": "0", "sinceQueryState": "0"}})
		if resp.Args["type"] != "accountNotFound" {
			t.Fatalf("%s: type = %v, want accountNotFound", m, resp.Args["type"])
		}
		if resp.Name != "error" {
			t.Errorf("%s: response name = %q, want \"error\" (RFC 8620 §3.6.2)", m, resp.Name)
		}
	}
}

func TestR235RequestOverMaxSizeRequestIsLimitError(t *testing.T) {
	if maxSizeRequest != 10*1024*1024 {
		t.Fatalf("maxSizeRequest = %d, session advertises 10 MiB", maxSizeRequest)
	}
	ts := apiLimitServer(t)
	post := func(body []byte) (int, map[string]interface{}) {
		req, _ := http.NewRequest("POST", ts.URL+"/jmap/api", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+apiLimitToken(t))
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	// Control: just under the advertised limit is processed.
	if code, _ := post(apiLimitRequestBody(t, maxSizeRequest-1024)); code != 200 {
		t.Fatalf("CONTROL FAILED: under-limit status = %d", code)
	}
	code, out := post(apiLimitRequestBody(t, maxSizeRequest+1))
	if code != 400 || out["type"] != "urn:ietf:params:jmap:error:limit" || out["limit"] != "maxSizeRequest" {
		t.Fatalf("over maxSizeRequest: status=%d body=%v, want 400 limit/maxSizeRequest", code, out)
	}
}

func TestR235MailboxDestroyRemoveEmailsReleasesBlobAndQuota(t *testing.T) {
	srv, db, store, cleanup := setupTestServer(t)
	defer cleanup()
	const user = "alice@example.com"
	for _, m := range []string{"INBOX", "Projects"} {
		if err := db.CreateMailbox(user, m); err != nil {
			t.Fatal(err)
		}
	}
	var used int64
	srv.SetQuotaAdjustFunc(func(_ string, d int64) error { used += d; return nil })

	only, _ := store.StoreMessage(user, []byte("Subject: only\r\n\r\nonly in projects\r\n"))
	shared, _ := store.StoreMessage(user, []byte("Subject: shared\r\n\r\nalso in inbox\r\n"))
	onlySize, _ := store.MessageSize(user, only)
	sharedSize, _ := store.MessageSize(user, shared)
	used = onlySize + sharedSize
	put := func(box, id string, uid uint32, size int64) {
		if err := db.StoreMessageMetadata(user, box, uid, &storage.MessageMetadata{MessageID: id, UID: uid, Size: size, InternalDate: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	put("Projects", only, 1, onlySize)
	put("Projects", shared, 2, sharedSize)
	put("INBOX", shared, 1, sharedSize)

	since, _ := db.CurrentChangeState(user)
	resp := srv.handleMailboxSet(user, MethodCall{Name: "Mailbox/set", ID: "c", Args: map[string]interface{}{
		"accountId": user, "destroy": []interface{}{"Projects"}, "onDestroyRemoveEmails": true,
	}})
	if d, _ := resp.Args["destroyed"].([]string); len(d) != 1 {
		t.Fatalf("destroy failed: %v", resp.Args)
	}
	// Control: the blob still referenced from INBOX survives.
	if _, err := store.ReadMessage(user, shared); err != nil {
		t.Fatalf("CONTROL FAILED: shared blob removed: %v", err)
	}
	if _, err := store.ReadMessage(user, only); err == nil {
		t.Errorf("blob of the removed email leaked on disk")
	}
	if used != sharedSize {
		t.Errorf("quota used = %d, want %d (released %d)", used, sharedSize, onlySize)
	}
	// The removed emails must appear as destroyed in Email/changes.
	ch := srv.handleChanges(user, MethodCall{Name: "Email/changes", ID: "c", Args: map[string]interface{}{"accountId": user, "sinceState": since}}, "Email/changes", storage.ChangeTypeEmail, false)
	destroyed, _ := ch.Args["destroyed"].([]string)
	found := false
	for _, id := range destroyed {
		if id == only {
			found = true
		}
	}
	if !found {
		t.Errorf("Email/changes destroyed = %v, missing %s", destroyed, only)
	}
}

func TestR235EmailImportHoldsReferenceGuard(t *testing.T) {
	srv, db, store, cleanup := setupTestServer(t)
	defer cleanup()
	const user = "alice@example.com"
	_ = db.CreateMailbox(user, "INBOX")
	blob, _ := store.StoreMessage(user, []byte("Subject: g\r\n\r\nbody\r\n"))

	gmu := srv.userGuard(user)
	gmu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleEmailImport(user, MethodCall{Name: "Email/import", ID: "i", Args: map[string]interface{}{
			"accountId": user,
			"emails":    map[string]interface{}{"k": map[string]interface{}{"blobId": blob, "mailboxIds": map[string]interface{}{"inbox": true}}},
		}})
	}()
	select {
	case <-done:
		gmu.Unlock()
		t.Fatal("Email/import completed while the reference guard was held (a concurrent Email/destroy could free the blob mid-import)")
	case <-time.After(200 * time.Millisecond):
	}
	gmu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Email/import never completed after the guard was released")
	}
	if uids, _ := db.GetMessageUIDs(user, "INBOX"); len(uids) != 1 {
		t.Fatalf("import stored %d messages, want 1", len(uids))
	}
}
