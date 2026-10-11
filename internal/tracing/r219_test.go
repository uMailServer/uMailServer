package tracing

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// F6961: path, host and user agent are client-controlled; bound them.
func TestTruncateAttr(t *testing.T) {
	if truncateAttr("/api/v1/x") != "/api/v1/x" {
		t.Fatal("short value altered")
	}
	got := truncateAttr(strings.Repeat("a", maxAttrLen-1) + "é" + strings.Repeat("b", 1<<15))
	if len(got) > maxAttrLen || !utf8.ValidString(got) {
		t.Fatalf("bad truncation: len=%d valid=%v", len(got), utf8.ValidString(got))
	}
}
