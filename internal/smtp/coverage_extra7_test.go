package smtp

import (
	"testing"
)

// --- extractUserFromRecipient tests ---

func TestExtractUserFromRecipient_Email(t *testing.T) {
	result := extractUserFromRecipient("user@example.com")
	if result != "user" {
		t.Errorf("Expected 'user', got %q", result)
	}
}

func TestExtractUserFromRecipient_Empty(t *testing.T) {
	result := extractUserFromRecipient("")
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}
}

func TestExtractUserFromRecipient_BangFormat(t *testing.T) {
	result := extractUserFromRecipient("user!otherdomain!mailbox")
	if result != "user" {
		t.Errorf("Expected 'user', got %q", result)
	}
}

func TestExtractUserFromRecipient_NoAt(t *testing.T) {
	result := extractUserFromRecipient("username")
	if result != "username" {
		t.Errorf("Expected 'username', got %q", result)
	}
}
