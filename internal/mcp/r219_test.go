package mcp

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// F6968: client-controlled method / id / tool strings are bounded before they
// become span names and attributes.
func TestSpanTextBoundsClientStrings(t *testing.T) {
	if got := spanText("tools/call"); got != "tools/call" {
		t.Fatalf("short value altered: %q", got)
	}
	long := strings.Repeat("a", maxSpanText-1) + "é" + strings.Repeat("b", 1<<16)
	got := spanText(long)
	if len(got) > maxSpanText {
		t.Fatalf("len = %d, want <= %d", len(got), maxSpanText)
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncation split a rune")
	}
}
