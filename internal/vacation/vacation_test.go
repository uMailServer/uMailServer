package vacation

import (
	"testing"
	"time"
)

func TestShouldReply_Enabled(t *testing.T) {
	settings := Settings{Enabled: true}
	headers := map[string][]string{}

	if !ShouldReply(settings, "user@example.com", headers) {
		t.Error("Should reply when enabled and no exclusions")
	}
}

func TestShouldReply_Disabled(t *testing.T) {
	settings := Settings{Enabled: false}
	headers := map[string][]string{}

	if ShouldReply(settings, "user@example.com", headers) {
		t.Error("Should not reply when disabled")
	}
}

func TestShouldReply_DateRange(t *testing.T) {
	now := time.Now()

	// Within range
	settings := Settings{
		Enabled:   true,
		StartTime: now.Add(-24 * time.Hour),
		EndTime:   now.Add(24 * time.Hour),
	}
	if !ShouldReply(settings, "user@example.com", nil) {
		t.Error("Should reply within date range")
	}

	// Before start
	settings2 := Settings{
		Enabled:   true,
		StartTime: now.Add(24 * time.Hour),
	}
	if ShouldReply(settings2, "user@example.com", nil) {
		t.Error("Should not reply before start time")
	}

	// After end
	settings3 := Settings{
		Enabled: true,
		EndTime: now.Add(-24 * time.Hour),
	}
	if ShouldReply(settings3, "user@example.com", nil) {
		t.Error("Should not reply after end time")
	}
}

func TestShouldReply_ExcludeBounces(t *testing.T) {
	settings := Settings{Enabled: true}
	headers := map[string][]string{}

	bounces := []string{
		"mailer-daemon@example.com",
		"postmaster@example.com",
		"bounce-123@example.com",
	}
	for _, from := range bounces {
		if ShouldReply(settings, from, headers) {
			t.Errorf("Should not reply to bounce address: %s", from)
		}
	}
}

func TestShouldReply_ExcludeMailingLists(t *testing.T) {
	settings := Settings{Enabled: true}

	tests := []map[string][]string{
		{"List-Id": {"dev.example.com"}},
		{"List-Unsubscribe": {"<mailto:unsubscribe@example.com>"}},
		{"Precedence": {"bulk"}},
		{"Precedence": {"junk"}},
	}
	for _, headers := range tests {
		if ShouldReply(settings, "list@example.com", headers) {
			t.Errorf("Should not reply to mailing list with headers: %v", headers)
		}
	}
}

func TestShouldReply_ExcludeAutoSubmitted(t *testing.T) {
	settings := Settings{Enabled: true}

	headers := map[string][]string{
		"Auto-Submitted": {"auto-generated"},
	}
	if ShouldReply(settings, "auto@example.com", headers) {
		t.Error("Should not reply to auto-generated messages")
	}

	headers2 := map[string][]string{
		"Auto-Submitted": {"auto-replied"},
	}
	if ShouldReply(settings, "auto@example.com", headers2) {
		t.Error("Should not reply to auto-replied messages")
	}
}

func TestShouldReply_ExcludeNoreply(t *testing.T) {
	settings := Settings{Enabled: true}
	headers := map[string][]string{}

	noreply := []string{
		"noreply@example.com",
		"no-reply@example.com",
	}
	for _, from := range noreply {
		if ShouldReply(settings, from, headers) {
			t.Errorf("Should not reply to noreply address: %s", from)
		}
	}
}

func TestGenerateReply(t *testing.T) {
	settings := Settings{
		Subject: "Out of Office",
		Body:    "I am away until Monday.",
	}

	msg := GenerateReply(settings, "sender@example.com", "me@example.com")
	msgStr := string(msg)

	if !contains(msgStr, "From: me@example.com") {
		t.Error("Reply should have correct From header")
	}
	if !contains(msgStr, "To: sender@example.com") {
		t.Error("Reply should have correct To header")
	}
	if !contains(msgStr, "Subject: Out of Office") {
		t.Error("Reply should have correct Subject header")
	}
	if !contains(msgStr, "I am away until Monday.") {
		t.Error("Reply should contain body text")
	}
	if !contains(msgStr, "Auto-Submitted: auto-replied") {
		t.Error("Reply should have Auto-Submitted header")
	}
	if !contains(msgStr, "Precedence: bulk") {
		t.Error("Reply should have Precedence: bulk")
	}
}

func TestGenerateReply_Defaults(t *testing.T) {
	settings := Settings{}
	msg := string(GenerateReply(settings, "sender@example.com", "me@example.com"))

	if !contains(msg, "Auto: Out of Office") {
		t.Errorf("Should use default subject, got: %s", msg[:200])
	}
	if !contains(msg, "currently out of the office") {
		t.Errorf("Should use default body, got: %s", msg[:200])
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
