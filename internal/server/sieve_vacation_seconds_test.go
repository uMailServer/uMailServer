package server

// Regression tests for the sieve-vacation :seconds gate defect: the delivery
// path gated vacation replies through sieve.Manager.CheckAndRecordVacation
// passing only VacationAction.Days (interpreter default: 7), and the
// manager's API is days-only with a 24h floor — so a script using RFC 6131's
// ":seconds" parameter was suppressed for 7 days regardless of its stated
// window, and the seconds-aware suppression inside handleSieveVacation was
// never reached. RFC 6131 §2: the ":seconds" value "is used instead of the
// ':days' value"; every value from 0 to 2**31-1 MUST be accepted without
// error; ":seconds 0" means all auto-replies are sent with no attempt to
// suppress consecutive replies. RFC 5230 remains the contract for the :days
// form (suppression within the :days window).

import (
	"testing"
	"time"
)

const sieveVacSender = "bob@remote.example"

func sieveVacReplies(t *testing.T, srv *Server) int {
	t.Helper()
	return len(sieveDTQueued(t, srv, sieveVacSender))
}

// ":seconds 0" means reply to every message from the same sender (RFC 6131
// §2). Sleep-free by design: the second delivery arrives immediately and
// must still produce a second reply.
func TestSieveVacationSecondsZeroRepliesToEveryMessage(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation-seconds\"];\nvacation :seconds 0 \"ack\";\n")

	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, junkRTClean)
	if got := sieveVacReplies(t, srv); got != 1 {
		t.Fatalf("CONTROL FAILED (harness): first :seconds 0 delivery queued %d replies, want 1", got)
	}

	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, junkRTClean)
	if got := sieveVacReplies(t, srv); got != 2 {
		t.Fatalf("FAIL: :seconds 0 must reply to every message (RFC 6131 §2), got %d replies after 2 deliveries — the script's :seconds window is being ignored", got)
	}
}

// A :seconds window of 1 second must expire: after 1.4s the same sender gets
// a second reply, and an immediate third delivery stays suppressed by the
// fresh 1-second window (the window gates, it does not just stop replying).
func TestSieveVacationSecondsWindowHonored(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation-seconds\"];\nvacation :seconds 1 \"brb\";\n")

	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, junkRTClean)
	if got := sieveVacReplies(t, srv); got != 1 {
		t.Fatalf("CONTROL FAILED (harness): first delivery queued %d replies, want 1", got)
	}

	time.Sleep(1400 * time.Millisecond) // bounded real wait; the 1s window must have elapsed (waits only overshoot)

	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, junkRTClean)
	if got := sieveVacReplies(t, srv); got != 2 {
		t.Fatalf("FAIL: :seconds 1 window ignored — after 1.4s got %d replies, want 2 (the 7-day days-form gate would suppress)", got)
	}

	// The fresh 1-second window must gate an immediate retry.
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, junkRTClean)
	if got := sieveVacReplies(t, srv); got != 2 {
		t.Fatalf("FAIL: fresh :seconds window does not gate: %d replies after immediate third delivery, want 2", got)
	}
}

// Control (must hold before and after the fix): the :days form still
// suppresses repeat replies within its window.
func TestSieveVacationDaysFormSuppressionKept(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation\"];\nvacation :days 7 \"ooo\";\n")

	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, junkRTClean)
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, junkRTClean)
	if got := sieveVacReplies(t, srv); got != 1 {
		t.Fatalf("CONTROL FAILED: :days 7 suppression broken: %d replies, want 1", got)
	}
}
