package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// F7770: the vacation a user saves through PUT /api/v1/vacation must drive the
// auto-reply on delivery, not only the admin-managed account setting.

const r302Msg = "From: sender@external.example\r\nTo: alice@test.com\r\nSubject: hi\r\nMessage-ID: <%s@external.example>\r\nDate: Mon, 19 Oct 2026 10:00:00 +0000\r\n\r\nhello\r\n"

func r302Queued(srv *Server) int {
	n := 0
	l, _ := srv.queue.GetPendingEntries()
	for _, e := range l {
		for _, to := range e.To {
			if strings.EqualFold(to, "sender@external.example") {
				n++
			}
		}
	}
	return n
}

// r302Deliver returns how many auto-replies the delivery queued.
func r302Deliver(t *testing.T, srv *Server, id string) int {
	t.Helper()
	before := r302Queued(srv)
	if err := srv.deliverMessageWithNotify("sender@external.example", []string{"alice@test.com"}, nil, []byte(fmt.Sprintf(r302Msg, id))); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	// the reply is queued from a background goroutine, which Stop waits for
	srv.bgWG.Wait()
	return r302Queued(srv) - before
}

func r302Put(t *testing.T, srv *Server, pw string, cfg map[string]any) {
	t.Helper()
	body, _ := json.Marshal(cfg)
	if rec := jwtRotDo(srv.apiServer, http.MethodPut, "/api/v1/vacation", jwtRotLogin(t, srv, pw), body); rec.Code != http.StatusOK {
		t.Fatalf("PUT vacation: %d %s", rec.Code, rec.Body.String())
	}
}

func TestVacationRESTSettingsAnswerMail(t *testing.T) {
	srv, pw := r290Server(t)
	if n := r302Deliver(t, srv, "before"); n != 0 {
		t.Fatalf("no vacation configured but %d replies queued", n)
	}
	r302Put(t, srv, pw, map[string]any{"enabled": true, "subject": "Away", "message": "Back Friday.", "send_interval": 24})
	if n := r302Deliver(t, srv, "m1"); n != 1 {
		t.Fatalf("REST-configured vacation queued %d replies, want 1", n)
	}
	if n := r302Deliver(t, srv, "m2"); n != 0 {
		t.Fatalf("second mail from the same sender inside the interval queued %d replies, want 0", n)
	}
}

func TestVacationRESTDisabledAndExpiredStaySilent(t *testing.T) {
	past := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	for name, cfg := range map[string]map[string]any{
		"disabled": {"enabled": false, "subject": "Away", "message": "Back Friday.", "send_interval": 24},
		"expired":  {"enabled": true, "subject": "Away", "message": "Back Friday.", "send_interval": 24, "end_date": past},
	} {
		t.Run(name, func(t *testing.T) {
			srv, pw := r290Server(t)
			r302Put(t, srv, pw, cfg)
			if n := r302Deliver(t, srv, name); n != 0 {
				t.Fatalf("%s vacation queued %d replies, want 0", name, n)
			}
		})
	}
}
