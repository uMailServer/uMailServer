package api

// Round 62 regression tests (the round-60 plan's Option B): the orphan-blob
// sweep admin endpoint POST /api/v1/admin/blob-gc. Auth matrix: anonymous →
// 401 (withAuth), authenticated non-admin → 403 (adminMiddleware); with a
// wired message store an admin POST runs one sweep and reports the per-owner
// outcome {"removed", "bytesFreed"} plus "totalRemoved"; without a message
// store it reports 503. The sweep drives the same storage machinery as the
// round-61 periodic sweep (BlobOwners + GCOrphanBlobs), so the reference
// checker and grace window protect referenced and fresh blobs.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// r62WithStore wires a fresh message store onto the test server and returns
// its base path (for aging blob files past the grace window).
func r62WithStore(t *testing.T, s *Server) string {
	t.Helper()
	tmp := t.TempDir()
	ms, err := storage.NewMessageStore(tmp + "/messages")
	if err != nil {
		t.Fatal(err)
	}
	// Production installs the reference checker at wiring time
	// (internal/jmap/server.go:164); this fixture mirrors that, since
	// GCOrphanBlobs refuses to run without one.
	ms.SetBlobReferenceChecker(func(user string, ids []string) (map[string]bool, error) {
		return make(map[string]bool), nil
	})
	s.SetMsgStore(ms)
	return tmp + "/messages"
}

func r62AgedOrphan(t *testing.T, ms *storage.MessageStore, basePath, user, content string) string {
	t.Helper()
	id, err := ms.StoreMessage(user, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(basePath, user, id[:2], id[2:4], id)
	stamp := time.Now().Add(-2 * time.Hour) // past the 1h orphanGCGrace
	if err := os.Chtimes(p, stamp, stamp); err != nil {
		t.Fatalf("age blob: %v", err)
	}
	return id
}

func TestAdminBlobGCRequiresAuth(t *testing.T) {
	srv, h := a4935Setup(t)
	_ = srv
	c, _ := a4935Do(h, http.MethodPost, "/api/v1/admin/blob-gc", "", "")
	if c != http.StatusUnauthorized {
		t.Fatalf("FAIL: anonymous POST /api/v1/admin/blob-gc status = %d, want 401", c)
	}
}

func TestAdminBlobGCForbiddenForNonAdmin(t *testing.T) {
	srv, h := a4935Setup(t)
	_ = srv
	tok := a4935Token(t, "bob@ex.com", false)
	c, _ := a4935Do(h, http.MethodPost, "/api/v1/admin/blob-gc", tok, "")
	if c != http.StatusForbidden {
		t.Fatalf("FAIL: non-admin POST /api/v1/admin/blob-gc status = %d, want 403", c)
	}
}

func TestAdminBlobGCUnavailableWithoutStore(t *testing.T) {
	_, h := a4935Setup(t)
	tok := a4935Token(t, "root@ex.com", true)
	c, _ := a4935Do(h, http.MethodPost, "/api/v1/admin/blob-gc", tok, "")
	if c != http.StatusServiceUnavailable {
		t.Fatalf("FAIL: store-less POST /api/v1/admin/blob-gc status = %d, want 503", c)
	}
}

func TestAdminBlobGCSweepExecutesAndShape(t *testing.T) {
	srv, h := a4935Setup(t)
	basePath := r62WithStore(t, srv)
	const user = "alice@example.com"
	if _, err := srv.msgStore.StoreMessage(user, []byte("aged orphan for the round-62 endpoint proof")); err != nil {
		t.Fatal(err)
	}
	r62AgedOrphan(t, srv.msgStore, basePath, user, "aged orphan for the round-62 endpoint proof")

	tok := a4935Token(t, "root@ex.com", true)
	c, raw := a4935Do(h, http.MethodPost, "/api/v1/admin/blob-gc", tok, "")
	if c != http.StatusOK {
		t.Fatalf("FAIL: admin POST /api/v1/admin/blob-gc status = %d (body: %s)", c, raw)
	}
	var body struct {
		TotalRemoved int `json:"totalRemoved"`
		Results      map[string]struct {
			Removed    int   `json:"removed"`
			BytesFreed int64 `json:"bytesFreed"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("FAIL: response is not the documented shape: %v (%s)", err, raw)
	}
	if body.TotalRemoved != 1 {
		t.Fatalf("FAIL: totalRemoved = %d, want 1 (body: %s)", body.TotalRemoved, raw)
	}
	r, ok := body.Results[user]
	if !ok || r.Removed != 1 || r.BytesFreed <= 0 {
		t.Fatalf("FAIL: results[%q] = %+v, want removed=1 and bytesFreed>0 (body: %s)", user, r, raw)
	}
}

// The endpoint only answers POST (the family's method-switch convention).
func TestAdminBlobGCMethodNotAllowed(t *testing.T) {
	_, h := a4935Setup(t)
	tok := a4935Token(t, "root@ex.com", true)
	c, _ := a4935Do(h, http.MethodGet, "/api/v1/admin/blob-gc", tok, "")
	if c != http.StatusMethodNotAllowed {
		t.Fatalf("FAIL: GET /api/v1/admin/blob-gc status = %d, want 405", c)
	}
}
