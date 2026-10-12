package server

import "testing"

// F7305b: calling the sender directly with the message's headers (as
// vacationInfoFromMessage extracts them) must also suppress the reply.
func TestR253VacationCtxHonoursHeaders_F7305(t *testing.T) {
	for name, h := range map[string]string{
		"auto-submitted": "Auto-Submitted: auto-replied\r\n",
		"precedence":     "Precedence: Bulk\r\n",
		"list-id":        "List-Id: <l.ext.com>\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			s := newDeliveryAuditServer(t)
			info := vacationInfoFromMessage([]byte("From: s@ext.com\r\nSubject: Hi\r\n" + h + "\r\nx\r\n"))
			// list headers are honoured under the ignore_lists option
			s.sendVacationReplyCtx("bob@test.com", "s@ext.com", "", info, `{"enabled":true,"message":"away","ignore_lists":true}`)
			if n := len(vacationQueued(t, s)); n != 0 {
				t.Fatalf("reply queued despite %s", name)
			}
		})
	}
	s := newDeliveryAuditServer(t)
	info := vacationInfoFromMessage([]byte("From: s@ext.com\r\nSubject: Hi\r\n\r\nx\r\n"))
	s.sendVacationReplyCtx("bob@test.com", "s@ext.com", "", info, round89Vacation)
	if n := len(vacationQueued(t, s)); n != 1 {
		t.Fatalf("control: want 1 reply, got %d", n)
	}
}

// F7305: the account-settings vacation path must pass the real message
// headers to vacation.Eligible so Auto-Submitted / Precedence / list headers
// suppress the reply (RFC 3834 §2).
func TestR253VacationSuppressedByHeaders_F7305(t *testing.T) {
	cases := map[string]string{
		"auto-submitted": "Auto-Submitted: auto-generated\r\n",
		"precedence":     "Precedence: bulk\r\n",
		"list-id":        "List-Id: <l.ext.com>\r\n",
		"list-unsub":     "List-Unsubscribe: <mailto:u@ext.com>\r\n",
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			s := newDeliveryAuditServer(t)
			round89SetVacation(t, s, "bob")
			msg := "From: s@ext.com\r\nTo: bob@test.com\r\nSubject: Hi\r\nMessage-ID: <m1@ext.com>\r\n" + h + "\r\nhello\r\n"
			if err := s.deliverMessage("s@ext.com", []string{"bob@test.com"}, []byte(msg)); err != nil {
				t.Fatal(err)
			}
			round89WaitIdle(t, s)
			if n := len(vacationQueued(t, s)); n != 0 {
				t.Fatalf("reply queued despite %s header", name)
			}
		})
	}
	// Control: an ordinary message still gets a reply, and Auto-Submitted: no does too.
	s := newDeliveryAuditServer(t)
	round89SetVacation(t, s, "bob")
	msg := "From: s@ext.com\r\nTo: bob@test.com\r\nSubject: Hi\r\nAuto-Submitted: no\r\n\r\nhello\r\n"
	if err := s.deliverMessage("s@ext.com", []string{"bob@test.com"}, []byte(msg)); err != nil {
		t.Fatal(err)
	}
	round89WaitIdle(t, s)
	if n := len(vacationQueued(t, s)); n != 1 {
		t.Fatalf("control: want 1 reply, got %d", n)
	}
}
