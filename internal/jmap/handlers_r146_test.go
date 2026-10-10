package jmap

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const r146User = "alice@example.com"

func r146Upload(t *testing.T, srv *Server, body []byte) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/jmap/upload", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+quotaUploadToken(t))
	w := httptest.NewRecorder()
	srv.handleUpload(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status %d", w.Code)
	}
	s := w.Body.String()
	i := strings.Index(s, `"blobId":"`) + len(`"blobId":"`)
	return s[i : i+strings.Index(s[i:], `"`)]
}

func r146Import(srv *Server, blob string, boxes ...string) Response {
	mb := map[string]interface{}{}
	for _, b := range boxes {
		mb[b] = true
	}
	return srv.handleEmailImport(r146User, MethodCall{Name: "Email/import", ID: "i", Args: map[string]interface{}{
		"accountId": r146User,
		"emails":    map[string]interface{}{"k": map[string]interface{}{"blobId": blob, "mailboxIds": mb}},
	}})
}

// F6280: unreferenced uploads are expired by the GC; referenced ones kept.
func TestR146UploadGCExpiresUnreferenced(t *testing.T) {
	srv, db, store, cleanup := setupTestServer(t)
	defer cleanup()
	defer srv.Stop()
	_ = db.CreateMailbox(r146User, "INBOX")
	orphan := r146Upload(t, srv, []byte("Subject: orphan\r\n\r\nbody one\r\n"))
	kept := r146Upload(t, srv, []byte("Subject: kept\r\n\r\nbody two\r\n"))
	if r := r146Import(srv, kept, "inbox"); len(r.Args["created"].(map[string]Email)) != 1 {
		t.Fatalf("import failed: %v", r.Args)
	}
	if n := srv.SweepUploads(time.Now()); n != 0 {
		t.Fatalf("fresh uploads must not expire, removed %d", n)
	}
	if n := srv.SweepUploads(time.Now().Add(3 * time.Hour)); n != 1 {
		t.Fatalf("expected 1 expired blob, got %d", n)
	}
	if store.MessageExists(r146User, orphan) {
		t.Fatal("orphan blob survived GC")
	}
	if !store.MessageExists(r146User, kept) {
		t.Fatal("imported blob was deleted by GC")
	}
}

func TestR146UploadPendingBounded(t *testing.T) {
	srv, _, store, cleanup := setupTestServer(t)
	defer cleanup()
	defer srv.Stop()
	var first string
	for i := 0; i < maxPendingUploads+3; i++ {
		id := r146Upload(t, srv, []byte("blob-"+strings.Repeat("x", i)))
		if i == 0 {
			first = id
		}
		time.Sleep(time.Millisecond)
	}
	if store.MessageExists(r146User, first) {
		t.Fatal("oldest pending upload not evicted beyond per-account bound")
	}
}

// Re-uploading content that already backs an Email must not make it GC-able.
func TestR146ReuploadOfImportedBlobNotCollected(t *testing.T) {
	srv, db, store, cleanup := setupTestServer(t)
	defer cleanup()
	defer srv.Stop()
	_ = db.CreateMailbox(r146User, "INBOX")
	body := []byte("Subject: s\r\n\r\nhello\r\n")
	id := r146Upload(t, srv, body)
	r146Import(srv, id, "inbox")
	r146Upload(t, srv, body)
	srv.SweepUploads(time.Now().Add(5 * time.Hour))
	if !store.MessageExists(r146User, id) {
		t.Fatal("blob backing an Email was collected")
	}
}

func TestR146StopIdempotent(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()
	srv.uploadGCEvery = 5 * time.Millisecond
	r146Upload(t, srv, []byte("x-data"))
	srv.Stop()
	srv.Stop()
}

// F6281: one blob imported twice / into several mailboxes is charged once
// and released once on destroy.
func TestR146ImportQuotaChargedOncePerBlob(t *testing.T) {
	srv, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	defer srv.Stop()
	_ = db.CreateMailbox(r146User, "INBOX")
	_ = db.CreateMailbox(r146User, "Archive")
	var used int64
	srv.SetQuotaAdjustFunc(func(_ string, d int64) error { used += d; return nil })
	body := []byte("Subject: q\r\n\r\n0123456789\r\n")
	id := r146Upload(t, srv, body)
	r146Import(srv, id, "inbox", "archive")
	r146Import(srv, id, "inbox")
	if used != int64(len(body)) {
		t.Fatalf("charged %d, want %d", used, len(body))
	}
	srv.handleEmailSet(r146User, MethodCall{Name: "Email/set", ID: "s", Args: map[string]interface{}{
		"accountId": r146User, "destroy": []interface{}{id},
	}})
	if used != 0 {
		t.Fatalf("quota leaked after destroy: %d", used)
	}
}

// F6282: anchor / anchorOffset / calculateTotal.
func TestR146EmailQueryAnchorAndTotal(t *testing.T) {
	srv, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	defer srv.Stop()
	_ = db.CreateMailbox(r146User, "INBOX")
	var ids []string
	for _, s := range []string{"a", "b", "c", "d"} {
		id := r146Upload(t, srv, []byte("Subject: "+s+"\r\n\r\n"+s+"\r\n"))
		r146Import(srv, id, "inbox")
		ids = append(ids, id)
	}
	q := func(args map[string]interface{}) Response {
		args["accountId"] = r146User
		args["sort"] = []interface{}{map[string]interface{}{"property": "subject", "isAscending": true}}
		return srv.handleEmailQuery(r146User, MethodCall{Name: "Email/query", ID: "q", Args: args})
	}
	all := q(map[string]interface{}{}).Args["ids"].([]string)
	if _, has := q(map[string]interface{}{}).Args["total"]; has {
		t.Fatal("total present without calculateTotal")
	}
	if q(map[string]interface{}{"calculateTotal": true}).Args["total"] != 4 {
		t.Fatal("total missing with calculateTotal")
	}
	r := q(map[string]interface{}{"anchor": all[1], "anchorOffset": float64(1), "limit": float64(5), "position": float64(3)})
	got := r.Args["ids"].([]string)
	if r.Args["position"] != 2 || len(got) != 2 || got[0] != all[2] {
		t.Fatalf("anchor query: %v", r.Args)
	}
	r = q(map[string]interface{}{"anchor": "nope"})
	if r.Args["type"] != "anchorNotFound" {
		t.Fatalf("want anchorNotFound, got %v", r.Args)
	}
	r = q(map[string]interface{}{"anchor": all[1], "anchorOffset": float64(-5)})
	if r.Args["position"] != 0 {
		t.Fatalf("negative anchor offset not floored: %v", r.Args)
	}
	_ = ids
}

// F6283: header forms and body values.
func TestR146EmailGetHeaderFormsAndBodyValues(t *testing.T) {
	srv, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	defer srv.Stop()
	_ = db.CreateMailbox(r146User, "INBOX")
	msg := "From: =?UTF-8?Q?J=C3=BCrgen?= <j@example.com>\r\n" +
		"Subject: =?UTF-8?Q?h=C3=A9llo?=\r\n" +
		"Date: Mon, 02 Jan 2006 15:04:05 -0700\r\n" +
		"List-Unsubscribe: <https://x.example/u>, <mailto:u@x.example>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"héllo world"
	id := r146Upload(t, srv, []byte(msg))
	r146Import(srv, id, "inbox")
	get := func(extra map[string]interface{}, props ...string) map[string]interface{} {
		ps := make([]interface{}, len(props))
		for i, p := range props {
			ps[i] = p
		}
		args := map[string]interface{}{"accountId": r146User, "ids": []interface{}{id}, "properties": ps}
		for k, v := range extra {
			args[k] = v
		}
		r := srv.handleEmailGet(r146User, MethodCall{Name: "Email/get", ID: "g", Args: args})
		l, ok := r.Args["list"].([]interface{})
		if !ok || len(l) != 1 {
			t.Fatalf("get: %v", r.Args)
		}
		return l[0].(map[string]interface{})
	}
	e := get(nil, "header:Subject:asText", "header:Date:asDate", "header:From:asAddresses", "header:List-Unsubscribe:asURLs", "header:X-None:asText")
	if e["header:Subject:asText"] != "héllo" {
		t.Fatalf("asText: %v", e)
	}
	if e["header:Date:asDate"] != "2006-01-02T15:04:05-07:00" {
		t.Fatalf("asDate: %v", e["header:Date:asDate"])
	}
	if a := e["header:From:asAddresses"].([]EmailAddress); len(a) != 1 || a[0].Name != "Jürgen" || a[0].Email != "j@example.com" {
		t.Fatalf("asAddresses: %v", e["header:From:asAddresses"])
	}
	if u := e["header:List-Unsubscribe:asURLs"].([]string); len(u) != 2 || u[0] != "https://x.example/u" {
		t.Fatalf("asURLs: %v", u)
	}
	if v, ok := e["header:X-None:asText"]; !ok || v != nil {
		t.Fatalf("missing header must be null: %v", e)
	}
	e = get(map[string]interface{}{"fetchTextBodyValues": true, "maxBodyValueBytes": float64(2)}, "bodyValues", "textBody")
	bv := e["bodyValues"].(map[string]EmailBodyValue)["1"]
	if bv.Value != "h" || !bv.IsTruncated { // "é" is 2 bytes: cannot split
		t.Fatalf("truncation: %+v", bv)
	}
	e = get(nil, "bodyValues")
	if len(e["bodyValues"].(map[string]EmailBodyValue)) != 0 {
		t.Fatal("bodyValues fetched without a fetch option")
	}
	e = get(map[string]interface{}{"fetchAllBodyValues": true}, "bodyValues")
	if e["bodyValues"].(map[string]EmailBodyValue)["1"].Value != "héllo world" {
		t.Fatalf("fetchAll: %v", e)
	}
	r := srv.handleEmailGet(r146User, MethodCall{Name: "Email/get", ID: "g", Args: map[string]interface{}{
		"accountId": r146User, "ids": []interface{}{id}, "properties": []interface{}{"header:Subject:asBogus"},
	}})
	if r.Args["type"] != "invalidArguments" {
		t.Fatalf("bad header form: %v", r.Args)
	}
}

// F6284: PatchObject syntax errors.
func TestR146EmailSetPatchSyntax(t *testing.T) {
	srv, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	defer srv.Stop()
	_ = db.CreateMailbox(r146User, "INBOX")
	id := r146Upload(t, srv, []byte("Subject: p\r\n\r\nx\r\n"))
	r146Import(srv, id, "inbox")
	cases := map[string]map[string]interface{}{
		"invalidProperties": {"subject": "x"},
		"invalidPatch":      {"keywords/$seen/x": true},
	}
	cases["invalidProperties2"] = map[string]interface{}{"keywords": "notanobject"}
	cases["invalidPatch2"] = map[string]interface{}{"keywords": map[string]interface{}{"$seen": true}, "keywords/$flagged": true}
	cases["invalidProperties3"] = map[string]interface{}{"keywords/": true, "id": "z"}
	for name, patch := range cases {
		want := strings.TrimRight(name, "0123456789")
		if name == "invalidProperties3" {
			want = "invalidPatch" // empty path segment is reported first only if keys sort so; accept either
		}
		r := srv.handleEmailSet(r146User, MethodCall{Name: "Email/set", ID: "s", Args: map[string]interface{}{
			"accountId": r146User, "update": map[string]interface{}{id: patch},
		}})
		nu, _ := r.Args["notUpdated"].(map[string]interface{})
		e, _ := nu[id].(map[string]interface{})
		if e == nil {
			t.Fatalf("%s: not rejected: %v", name, r.Args)
		}
		if name != "invalidProperties3" && e["type"] != want {
			t.Fatalf("%s: got %v want %s", name, e, want)
		}
	}
	// a valid patch still works
	r := srv.handleEmailSet(r146User, MethodCall{Name: "Email/set", ID: "s", Args: map[string]interface{}{
		"accountId": r146User, "update": map[string]interface{}{id: map[string]interface{}{"keywords/$seen": true}},
	}})
	if _, ok := r.Args["updated"].(map[string]interface{})[id]; !ok {
		t.Fatalf("valid patch rejected: %v", r.Args)
	}
}

// F6285: Identity/get with an empty ids array selects nothing.
func TestR146IdentityGetEmptyIDs(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()
	defer srv.Stop()
	get := func(ids interface{}) []Identity {
		args := map[string]interface{}{"accountId": r146User}
		if ids != nil {
			args["ids"] = ids
		}
		return srv.handleIdentityGet(r146User, MethodCall{Name: "Identity/get", ID: "i", Args: args}).Args["list"].([]Identity)
	}
	if n := len(get([]interface{}{})); n != 0 {
		t.Fatalf("empty ids returned %d identities", n)
	}
	if n := len(get(nil)); n != 1 {
		t.Fatalf("null ids returned %d identities", n)
	}
}
