package cli

import (
	"testing"
)

// --- extractTargetUser tests ---

func TestExtractTargetUser_FromHeader(t *testing.T) {
	mm := NewMigrationManager(nil, nil, nil)

	data := `From sender@example.com Mon Jan 01 00:00:00 2024
From: <sender@example.com>
Subject: Test

Body`

	result := mm.extractTargetUser(data, "INBOX")
	if result != "sender@example.com" {
		t.Errorf("Expected 'sender@example.com', got %q", result)
	}
}

func TestExtractTargetUser_NoFromHeader_FolderFallback(t *testing.T) {
	mm := NewMigrationManager(nil, nil, nil)

	data := `From sender@example.com Mon Jan 01 00:00:00 2024
Subject: Test

Body`

	result := mm.extractTargetUser(data, "INBOX")
	if result != "INBOX" {
		t.Errorf("Expected 'INBOX' (folder fallback), got %q", result)
	}
}

func TestExtractTargetUser_NoFromHeader_NoFolder(t *testing.T) {
	mm := NewMigrationManager(nil, nil, nil)

	data := `From sender@example.com Mon Jan 01 00:00:00 2024
Subject: Test

Body`

	result := mm.extractTargetUser(data, "")
	if result != "unknown" {
		t.Errorf("Expected 'unknown', got %q", result)
	}
}

func TestExtractTargetUser_FromHeaderWithSpaces(t *testing.T) {
	mm := NewMigrationManager(nil, nil, nil)

	data := `From: "John Doe" <john.doe@example.com>
Subject: Test

Body`

	result := mm.extractTargetUser(data, "INBOX")
	if result != "john.doe@example.com" {
		t.Errorf("Expected 'john.doe@example.com', got %q", result)
	}
}

func TestExtractTargetUser_FromHeaderNoAngleBrackets(t *testing.T) {
	mm := NewMigrationManager(nil, nil, nil)

	data := `From: sender@example.com
Subject: Test

Body`

	result := mm.extractTargetUser(data, "INBOX")
	// No angle brackets, so it falls back to folder
	if result != "INBOX" {
		t.Errorf("Expected 'INBOX' (no angle brackets fallback), got %q", result)
	}
}
