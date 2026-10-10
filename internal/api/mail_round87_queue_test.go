package api

// Round 87 F5691: the admin queue detail routes are also mounted under
// /api/v1/admin/queue/, but the handler only trimmed /api/v1/queue/, so the
// whole path became the entry id and every get/retry/drop answered 404.
// F5692: dropping an entry through the API removed only the DB row; the queued
// message file stayed on disk (queue.Manager.DropEntry removes it).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func f5691Entry(t *testing.T) (*Server, string, string) {
	t.Helper()
	srv, qm := webmailSendServer(t)
	_, err := qm.Enqueue("sender@sendtest.invalid", []string{"rcpt@remote.example"}, []byte("Subject: x\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := qm.GetPendingEntries()
	if err != nil || len(entries) != 1 {
		t.Fatalf("pending entries = %d, err %v", len(entries), err)
	}
	return srv, entries[0].ID, entries[0].MessagePath
}

func f5691Do(srv *Server, method, url string) int {
	rec := httptest.NewRecorder()
	srv.handleQueueDetail(rec, httptest.NewRequest(method, url, nil))
	return rec.Code
}

func TestAudit5691Control(t *testing.T) {
	srv, id, _ := f5691Entry(t)
	if c := f5691Do(srv, http.MethodGet, "/api/v1/queue/"+id); c != http.StatusOK {
		t.Fatalf("legacy route get = %d", c)
	}
	if c := f5691Do(srv, http.MethodGet, "/api/v1/queue/missing"); c != http.StatusNotFound {
		t.Fatalf("missing id = %d", c)
	}
}

func TestAudit5691Failure(t *testing.T) {
	srv, id, _ := f5691Entry(t)
	c := f5691Do(srv, http.MethodGet, "/api/v1/admin/queue/"+id)
	t.Logf("EXPECTED: 200 ACTUAL: %d", c)
	if c != http.StatusOK {
		t.Errorf("DEFECT F5691: GET /api/v1/admin/queue/{id} = %d", c)
	}
}

func TestAudit5691Edges(t *testing.T) {
	srv, id, _ := f5691Entry(t)
	if c := f5691Do(srv, http.MethodPost, "/api/v1/admin/queue/"+id); c != http.StatusOK {
		t.Errorf("admin retry = %d", c)
	}
	if c := f5691Do(srv, http.MethodGet, "/api/v1/admin/queue/missing"); c != http.StatusNotFound {
		t.Errorf("admin missing id = %d", c)
	}
	if c := f5691Do(srv, http.MethodDelete, "/api/v1/admin/queue/"+id); c != http.StatusNoContent {
		t.Errorf("admin drop = %d", c)
	}
	if c := f5691Do(srv, http.MethodGet, "/api/v1/admin/queue/"+id); c != http.StatusNotFound {
		t.Errorf("dropped entry still found: %d", c)
	}
}

func TestAudit5692Control(t *testing.T) {
	_, _, path := f5691Entry(t)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("queued message file missing before drop: %v", err)
	}
}

func TestAudit5692Failure(t *testing.T) {
	srv, id, path := f5691Entry(t)
	if c := f5691Do(srv, http.MethodDelete, "/api/v1/queue/"+id); c != http.StatusNoContent {
		t.Fatalf("drop = %d", c)
	}
	_, err := os.Stat(path)
	t.Logf("EXPECTED: message file removed ACTUAL: stat err=%v", err)
	if err == nil {
		t.Errorf("DEFECT F5692: dropped queue entry left its message file %s on disk", path)
	}
}
