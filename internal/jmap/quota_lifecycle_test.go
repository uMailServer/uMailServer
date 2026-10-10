package jmap

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Regression tests for F5740-F5742, F5746: quota enforcement/release on the
// JMAP blob upload, Email/import and Email/destroy paths, and the 413 status
// for an oversized upload.

func quotaUploadToken(t *testing.T) string {
	t.Helper()
	tk := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "alice@example.com", "exp": time.Now().Add(time.Hour).Unix()})
	s, err := tk.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func quotaUpload(t *testing.T, srv *Server, n int, fill byte) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/jmap/upload", bytes.NewReader(bytes.Repeat([]byte{fill}, n)))
	req.Header.Set("Authorization", "Bearer "+quotaUploadToken(t))
	w := httptest.NewRecorder()
	srv.handleUpload(w, req)
	return w.Code
}

func TestUploadEnforcesQuotaLimit(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()
	srv.SetQuotaLimitFunc(func(string) int64 { return 150 })
	if c := quotaUpload(t, srv, 100, 'a'); c != http.StatusCreated {
		t.Fatalf("within quota: status %d", c)
	}
	if c := quotaUpload(t, srv, 100, 'b'); c != http.StatusRequestEntityTooLarge {
		t.Fatalf("over quota: status %d, want 413", c)
	}
	// Re-uploading identical content adds no bytes and is accepted.
	if c := quotaUpload(t, srv, 100, 'a'); c != http.StatusCreated {
		t.Fatalf("duplicate content: status %d", c)
	}
}

func TestUploadOversizeIs413(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()
	if c := quotaUpload(t, srv, 50<<20+1, 'x'); c != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", c)
	}
	if c := quotaUpload(t, srv, 10, 'x'); c != http.StatusCreated {
		t.Fatalf("small upload status %d", c)
	}
}

func TestEmailImportReservesAndDestroyReleasesQuota(t *testing.T) {
	srv, db, store, cleanup := setupTestServer(t)
	defer cleanup()
	const user = "alice@example.com"
	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	var used int64
	srv.SetQuotaAdjustFunc(func(_ string, d int64) error {
		if d > 0 && used+d > 60 {
			return errors.New("quota exceeded")
		}
		used += d
		return nil
	})
	body := []byte("Subject: a\r\n\r\n0123456789\r\n")
	big := append(append([]byte{}, body...), bytes.Repeat([]byte("x"), 80)...)
	b1, _ := store.StoreMessage(user, body)
	b2, _ := store.StoreMessage(user, big)
	imp := func(blob string) Response {
		return srv.handleEmailImport(user, MethodCall{Name: "Email/import", ID: "i", Args: map[string]interface{}{
			"emails": map[string]interface{}{"k": map[string]interface{}{"blobId": blob, "mailboxIds": map[string]interface{}{"inbox": true}}},
		}})
	}
	r := imp(b1)
	created, _ := r.Args["created"].(map[string]Email)
	if len(created) != 1 || used != int64(len(body)) {
		t.Fatalf("import: created=%v used=%d", r.Args, used)
	}
	r2 := imp(b2)
	nc, _ := r2.Args["notCreated"].(map[string]interface{})
	e, _ := nc["k"].(map[string]interface{})
	if e["type"] != "overQuota" || used != int64(len(body)) {
		t.Fatalf("over-quota import: %v used=%d", r2.Args, used)
	}
	set := srv.handleEmailSet(user, MethodCall{Name: "Email/set", ID: "s", Args: map[string]interface{}{
		"destroy": []interface{}{created["k"].ID},
	}})
	if d, _ := set.Args["destroyed"].([]string); len(d) != 1 || used != 0 {
		t.Fatalf("destroy: %v used=%d", set.Args, used)
	}
}
