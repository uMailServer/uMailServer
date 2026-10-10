package store

import (
	"strings"
	"testing"
)

func TestF5953_SanitizeHostname(t *testing.T) {
	got := sanitizeHostname("a/b:c\\d")
	if strings.ContainsAny(got, "/:\\") {
		t.Fatalf("unsafe hostname %q", got)
	}
}
