package sieve

// Boundary tests for the duration-aware vacation gate (RFC 6131 :seconds).
// CheckAndRecordVacationFor honors the window exactly and treats a window of
// 0 or less as "reply to every delivery" (:seconds 0 disables suppression);
// the legacy CheckAndRecordVacation keeps RFC 5230 :days semantics with its
// 24h floor. The end-to-end behavior (a :seconds script driving delivery) is
// pinned by internal/server/sieve_vacation_seconds_test.go.

import (
	"testing"
	"time"
)

func TestCheckAndRecordVacationFor_ZeroWindowRepliesEveryTime(t *testing.T) {
	m := NewManager()
	for i := 0; i < 3; i++ {
		if !m.CheckAndRecordVacationFor("s0@example.com", 0) {
			t.Fatalf("FAIL: :seconds 0 must reply to every delivery, delivery %d suppressed", i+1)
		}
	}
}

func TestCheckAndRecordVacationFor_NegativeWindowAlwaysAllows(t *testing.T) {
	m := NewManager()
	for i := 0; i < 2; i++ {
		if !m.CheckAndRecordVacationFor("sneg@example.com", -time.Second) {
			t.Fatalf("FAIL: negative window must always allow, call %d suppressed", i+1)
		}
	}
}

func TestCheckAndRecordVacationFor_ExactSecondWindow(t *testing.T) {
	m := NewManager()
	if !m.CheckAndRecordVacationFor("s1@example.com", time.Second) {
		t.Fatalf("CONTROL FAILED (harness): first call must be allowed")
	}
	if m.CheckAndRecordVacationFor("s1@example.com", time.Second) {
		t.Fatalf("FAIL: second call inside the 1s window was allowed")
	}
	// Deterministic time travel: 1s window expired.
	m.vacationCacheMu.Lock()
	m.vacationCache["s1@example.com"] = time.Now().Add(-time.Second)
	m.vacationCacheMu.Unlock()
	if !m.CheckAndRecordVacationFor("s1@example.com", time.Second) {
		t.Fatalf("FAIL: window expiry not honored — call after the 1s window was suppressed")
	}
}

func TestCheckAndRecordVacation_DaysFormKeeps24hFloorAndWindow(t *testing.T) {
	m := NewManager()
	// The legacy days-form floors any interval at 24h (RFC 5230 :days).
	if !m.CheckAndRecordVacation("days0@example.com", 0) {
		t.Fatalf("CONTROL FAILED (harness): first call must be allowed")
	}
	if m.CheckAndRecordVacation("days0@example.com", 0) {
		t.Fatalf("FAIL: legacy 24h floor lost — immediate second call was allowed")
	}
	// A multi-day window spans exactly that many days.
	if !m.CheckAndRecordVacation("days7@example.com", 7) {
		t.Fatalf("CONTROL FAILED (harness): first 7-day call must be allowed")
	}
	m.vacationCacheMu.Lock()
	m.vacationCache["days7@example.com"] = time.Now().Add(-6 * 24 * time.Hour)
	m.vacationCacheMu.Unlock()
	if m.CheckAndRecordVacation("days7@example.com", 7) {
		t.Fatalf("FAIL: 7-day window not honored — call after 6 days was allowed")
	}
	m.vacationCacheMu.Lock()
	m.vacationCache["days7@example.com"] = time.Now().Add(-8 * 24 * time.Hour)
	m.vacationCacheMu.Unlock()
	if !m.CheckAndRecordVacation("days7@example.com", 7) {
		t.Fatalf("FAIL: 7-day window never expired — call after 8 days was suppressed")
	}
}
