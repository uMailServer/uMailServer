package spam

import (
	"strings"
	"testing"
)

// F6496: body tokenization is bounded so a huge message cannot produce an
// unbounded token slice / per-token DB lookups.
func TestExtractTokensFromBody_Bounded_F6496(t *testing.T) {
	body := []byte(strings.Repeat("alpha beta gamma delta ", 400000)) // ~9 MB
	toks := ExtractTokensFromBody(body)
	if len(toks) > maxBodyTokens {
		t.Fatalf("got %d tokens, cap %d", len(toks), maxBodyTokens)
	}
}

// F6496: repeated tokens count once per message in training and classification.
func TestDedupTokens_F6496(t *testing.T) {
	got := dedupTokens([]string{"aaa", "bbb", "aaa", "aaa", "bbb"})
	if len(got) != 2 || got[0] != "aaa" || got[1] != "bbb" {
		t.Fatalf("got %v", got)
	}
}

// F6497: token length limits count characters, not UTF-8 bytes.
func TestIsValidToken_RuneLength_F6497(t *testing.T) {
	tk := NewTokenizer()
	if tk.isValidToken("日本") { // 2 runes, 6 bytes
		t.Error("2-rune token accepted")
	}
	if !tk.isValidToken("日本語") {
		t.Error("3-rune token rejected")
	}
	if !tk.isValidToken(strings.Repeat("é", 20)) { // 20 runes, 40 bytes
		t.Error("20-rune token rejected")
	}
	if tk.isValidToken(strings.Repeat("é", 21)) {
		t.Error("21-rune token accepted")
	}
}
