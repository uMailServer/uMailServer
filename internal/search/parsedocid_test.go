package search

import (
	"strings"
	"testing"
)

func TestParseDocID_Uint32Overflow(t *testing.T) {
	// uid=4294967296 overflows uint32 (4294967295 is max)
	_, _, err := parseDocID("INBOX:4294967296")
	if err == nil {
		t.Error("parseDocID should reject uid=4294967296 (exceeds uint32 max), got nil error")
	}
}

func TestParseDocID_MaxUint32(t *testing.T) {
	// uid=4294967295 is exactly uint32 max — should succeed
	folder, uid, err := parseDocID("INBOX:4294967295")
	if err != nil {
		t.Errorf("parseDocID should accept uid=4294967295, got error: %v", err)
	}
	if folder != "INBOX" || uid != 4294967295 {
		t.Errorf("parseDocID returned folder=%q uid=%d, want INBOX 4294967295", folder, uid)
	}
}

func TestGeneratePreview_NegativeMaxLen(t *testing.T) {
	// generatePreview slices content[:maxLen]; if maxLen < 0, this panics
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("generatePreview panicked with maxLen=-5: %v", r)
		}
	}()
	result := generatePreview("Hello World", -5)
	// Should return something safe, not panic
	_ = result
}

func TestGeneratePreview_ZeroMaxLen(t *testing.T) {
	result := generatePreview("Hello World", 0)
	if result != "" {
		t.Errorf("generatePreview with maxLen=0: got %q, want empty string", result)
	}
}

func TestGeneratePreview_ValidTruncation(t *testing.T) {
	content := strings.Repeat("a", 100)
	result := generatePreview(content, 10)
	if len(result) != 13 { // 10 chars + "..."
		t.Errorf("generatePreview truncated: got len=%d, want 13", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("generatePreview should add ... suffix, got %q", result)
	}
}
