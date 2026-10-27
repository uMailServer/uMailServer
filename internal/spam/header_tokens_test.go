package spam

// F7779: header tokens were unbounded, so one message with padded headers
// trained tens of thousands of tokens while Learn held the classifier lock.

import (
	"fmt"
	"strings"
	"testing"
)

func paddedMessage(words int) []byte {
	var b strings.Builder
	b.WriteString("From: spammer@bad.example\r\nSubject: zzuniquesubject offer\r\n")
	for i := 0; i < words/50; i++ {
		b.WriteString("X-Pad: ")
		for j := 0; j < 50; j++ {
			fmt.Fprintf(&b, "w%dx%d ", i, j)
		}
		b.WriteString("\r\n")
	}
	b.WriteString("\r\nshort body text\r\n")
	return []byte(b.String())
}

func TestHeaderTokensAreBounded(t *testing.T) {
	raw := paddedMessage(40000)
	tokens := MessageTokens(raw)
	if len(tokens) > maxBodyTokens+maxHeaderTokens {
		t.Fatalf("%d tokens from one message", len(tokens))
	}
	headers := map[string][]string{"X-Pad": {strings.Repeat("pad ", 50000)}, "Subject": {"zzuniquesubject"}, "From": {"spammer@bad.example"}}
	ht := ExtractTokensFromHeaders(headers)
	if len(ht) > maxHeaderTokens {
		t.Fatalf("%d header tokens", len(ht))
	}
	have := map[string]bool{}
	for _, tok := range ht {
		have[tok] = true
	}
	if !have["zzuniquesubject"] || !have["spammer@bad.example"] {
		t.Fatalf("padding crowded out Subject/From: %v", ht[:5])
	}
	// The same input gives the same tokens (headers are taken in a fixed order).
	if again := ExtractTokensFromHeaders(headers); strings.Join(again, " ") != strings.Join(ht, " ") {
		t.Fatal("header tokens depend on map order")
	}
}
