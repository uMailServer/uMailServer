package smtp

import (
	"testing"
)

// --- parseRcptToWithNotify tests ---

func TestParseRcptToWithNotify_Basic(t *testing.T) {
	addr, notify, err := parseRcptToWithNotify("TO:<user@example.com>")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if addr != "user@example.com" {
		t.Errorf("Expected address 'user@example.com', got %q", addr)
	}
	if notify != "" {
		t.Errorf("Expected empty notify, got %q", notify)
	}
}

func TestParseRcptToWithNotify_WithNotify(t *testing.T) {
	addr, notify, err := parseRcptToWithNotify("TO:<user@example.com> NOTIFY=SUCCESS,DELAY")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if addr != "user@example.com" {
		t.Errorf("Expected address 'user@example.com', got %q", addr)
	}
	if notify != "SUCCESS,DELAY" {
		t.Errorf("Expected notify 'SUCCESS,DELAY', got %q", notify)
	}
}

func TestParseRcptToWithNotify_LowercaseTo(t *testing.T) {
	addr, notify, err := parseRcptToWithNotify("to:<user@example.com>")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if addr != "user@example.com" {
		t.Errorf("Expected address 'user@example.com', got %q", addr)
	}
	_ = notify
}

func TestParseRcptToWithNotify_InvalidFormat(t *testing.T) {
	_, _, err := parseRcptToWithNotify("user@example.com")
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

func TestParseRcptToWithNotify_EmptyAddress(t *testing.T) {
	_, _, err := parseRcptToWithNotify("TO:<>")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestParseRcptToWithNotify_WithExtraSpaces(t *testing.T) {
	addr, _, err := parseRcptToWithNotify("TO:   <user@example.com>   ")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if addr != "user@example.com" {
		t.Errorf("Expected address 'user@example.com', got %q", addr)
	}
}

func TestParseRcptToWithNotify_NotifyOnlySuccess(t *testing.T) {
	addr, notify, err := parseRcptToWithNotify("TO:<user@example.com> NOTIFY=SUCCESS")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if addr != "user@example.com" {
		t.Errorf("Expected address 'user@example.com', got %q", addr)
	}
	if notify != "SUCCESS" {
		t.Errorf("Expected notify 'SUCCESS', got %q", notify)
	}
}

func TestParseRcptToWithNotify_NotifyNever(t *testing.T) {
	addr, notify, err := parseRcptToWithNotify("TO:<user@example.com> NOTIFY=NEVER")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if addr != "user@example.com" {
		t.Errorf("Expected address 'user@example.com', got %q", addr)
	}
	if notify != "NEVER" {
		t.Errorf("Expected notify 'NEVER', got %q", notify)
	}
}
