package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/storage"
)

// F7787: GET /api/v1/stats reported "messages": 0 whatever was stored.
func TestStatsCountsStoredMessages(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.CreateDomain(&db.DomainData{Name: "ex.com", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	s := NewServer(database, nil, Config{JWTSecret: adminMetricsSecret, TokenExpiry: time.Hour})
	seedSessionAccount(t, database, "x@ex.com", true)
	seedSessionAccount(t, database, "y@ex.com", false)
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/m.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mailDB.Close() })
	s.SetMailDB(mailDB)
	put := func(user, mailbox string, uid uint32) {
		if err := mailDB.CreateMailbox(user, mailbox); err != nil {
			t.Fatal(err)
		}
		if err := mailDB.StoreMessageMetadata(user, mailbox, uid, &storage.MessageMetadata{UID: uid, MessageID: fmt.Sprintf("%s-%s-%d", user, mailbox, uid)}); err != nil {
			t.Fatal(err)
		}
	}
	put("x@ex.com", "INBOX", 1)
	put("x@ex.com", "INBOX", 2)
	put("y@ex.com", "INBOX", 1)
	put("y@ex.com", "Sent", 1)
	put("y@ex.com", "Work", 1)

	rec := contactsDo(t, s, http.MethodGet, "/api/v1/stats", "", true)
	var out struct{ Messages int }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusOK || out.Messages != 5 {
		t.Fatalf("stats: %d messages = %d, want 5", rec.Code, out.Messages)
	}

	// The count is cached briefly (the dashboard polls); it follows new mail
	// once the cache expires.
	put("y@ex.com", "INBOX", 2)
	count := func() int {
		var o struct{ Messages int }
		_ = json.Unmarshal(contactsDo(t, s, http.MethodGet, "/api/v1/stats", "", true).Body.Bytes(), &o)
		return o.Messages
	}
	if n := count(); n != 5 {
		t.Fatalf("count not cached: %d", n)
	}
	s.msgCountAt = time.Now().Add(-2 * storedMessageCacheTTL)
	if n := count(); n != 6 {
		t.Fatalf("count after the cache expired: %d, want 6", n)
	}
}
