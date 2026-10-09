package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/sieve"
)

// vacationTestServer returns a server with an unstarted queue manager so that
// enqueued auto-replies can be counted without any network delivery.
func vacationTestServer(t *testing.T) *Server {
	t.Helper()
	srv := helperServer(t)
	srv.queue = queue.NewManager(srv.database, nil, filepath.Join(t.TempDir(), "q"), nil)
	return srv
}

func vacationQueued(t *testing.T, srv *Server) []string {
	t.Helper()
	entries, err := srv.queue.GetPendingEntries()
	if err != nil {
		t.Fatalf("GetPendingEntries: %v", err)
	}
	var out []string
	for _, e := range entries {
		b, err := os.ReadFile(e.MessagePath)
		if err != nil {
			t.Fatalf("read queued message: %v", err)
		}
		out = append(out, string(b))
	}
	return out
}

// ageVacationReplies simulates d of wall-clock time passing for every dedup entry.
func ageVacationReplies(srv *Server, d time.Duration) {
	srv.vacationRepliesMu.Lock()
	defer srv.vacationRepliesMu.Unlock()
	for k, v := range srv.vacationReplies {
		srv.vacationReplies[k] = v.Add(-d)
	}
}

// F4825: a message outside the vacation window gets no reply and must not
// suppress the first reply once the window is open.
func TestSendVacationReply_OutOfWindowDoesNotSuppress(t *testing.T) {
	srv := vacationTestServer(t)
	future := time.Now().Add(48 * time.Hour).Format("2006-01-02")
	srv.sendVacationReply("me@example.com", "a@example.org", `{"enabled":true,"message":"away","start_date":"`+future+`"}`)
	if n := len(vacationQueued(t, srv)); n != 0 {
		t.Fatalf("pre-start call enqueued %d replies", n)
	}
	srv.sendVacationReply("me@example.com", "a@example.org", `{"enabled":true,"message":"away"}`)
	if n := len(vacationQueued(t, srv)); n != 1 {
		t.Fatalf("expected 1 reply once the window is open, got %d", n)
	}
}

// F4826: cleanup must not drop a dedup entry whose send_interval (168h) is
// longer than the 48h cleanup retention.
func TestSendVacationReply_LongIntervalSurvivesCleanup(t *testing.T) {
	srv := vacationTestServer(t)
	week := `{"enabled":true,"message":"away","send_interval":604800000000000}`
	srv.sendVacationReply("me@example.com", "a@example.org", week)
	ageVacationReplies(srv, 49*time.Hour)
	srv.cleanupVacationReplies()
	srv.sendVacationReply("me@example.com", "a@example.org", week)
	if n := len(vacationQueued(t, srv)); n != 1 {
		t.Fatalf("expected 1 reply within 168h, got %d", n)
	}
	ageVacationReplies(srv, 120*time.Hour) // 169h total
	srv.cleanupVacationReplies()
	srv.sendVacationReply("me@example.com", "a@example.org", week)
	if n := len(vacationQueued(t, srv)); n != 2 {
		t.Fatalf("expected a second reply after 168h, got %d", n)
	}
}

// F4827: the Sieve vacation reply carries Auto-Submitted and Date headers.
func TestHandleSieveVacation_AutoSubmittedAndDate(t *testing.T) {
	srv := vacationTestServer(t)
	srv.handleSieveVacation("a@example.org", "me@example.com", sieve.VacationAction{Subject: "Away", Body: "back soon", Days: 7})
	msgs := vacationQueued(t, srv)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(msgs))
	}
	head, _, _ := strings.Cut(msgs[0], "\r\n\r\n")
	if !strings.Contains(head, "\r\nAuto-Submitted: auto-replied") || !strings.Contains(head, "\r\nDate: ") {
		t.Fatalf("sieve vacation reply missing Auto-Submitted/Date: %q", head)
	}
}
