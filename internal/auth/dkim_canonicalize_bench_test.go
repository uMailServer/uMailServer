package auth

import (
	"bytes"
	"strings"
	"testing"
)

// TestCanonicalizeBodyRelaxed_RFCExamples covers the worked examples in
// RFC 6376 §3.4.5 so the byte-level rewrite can't drift from the spec.
func TestCanonicalizeBodyRelaxed_RFCExamples(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "RFC 6376 §3.4.5 example",
			input: " C \r\nD \t E\r\n\r\n\r\n",
			want:  " C\r\nD E\r\n",
		},
		{
			name:  "tab collapsed to single space (mid-line)",
			input: "foo\tbar\r\n",
			want:  "foo bar\r\n",
		},
		{
			name:  "mixed space + tab run collapsed",
			input: "foo \t  \tbar\r\n",
			want:  "foo bar\r\n",
		},
		{
			name:  "leading tab preserved as single space",
			input: "\t\thello\r\n",
			want:  " hello\r\n",
		},
		{
			name:  "trailing tab stripped",
			input: "hello\t\t\r\n",
			want:  "hello\r\n",
		},
		{
			name:  "all-whitespace line becomes empty",
			input: "   \t  \r\n",
			want:  "\r\n",
		},
		{
			name:  "no trailing CRLF gains one",
			input: "abc",
			want:  "abc\r\n",
		},
		{
			name:  "LF-only line endings honoured (lenient input)",
			input: "abc\n",
			want:  "abc\r\n",
		},
		{
			name:  "preserves CR in mid-line as-is",
			input: "ab\rcd\r\n",
			want:  "ab\rcd\r\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := canonicalizeBodyRelaxed([]byte(tc.input))
			if !bytes.Equal(got, []byte(tc.want)) {
				t.Errorf("canonicalizeBodyRelaxed(%q)\n got = %q\nwant = %q", tc.input, string(got), tc.want)
			}
		})
	}
}

// TestCanonicalizeBodyRelaxed_DoesNotMutateInput proves the implementation
// returns a fresh buffer and never aliases the caller's body. This matters
// because callers (the DKIM signing path) reuse the body for content
// delivery after signing.
func TestCanonicalizeBodyRelaxed_DoesNotMutateInput(t *testing.T) {
	input := []byte("hello   world\r\n\r\n\r\n")
	original := append([]byte(nil), input...)
	_ = canonicalizeBodyRelaxed(input)
	if !bytes.Equal(input, original) {
		t.Errorf("input mutated:\n got = %q\nwant = %q", string(input), string(original))
	}
}

func benchBodyForCanon(size int) []byte {
	// Realistic-ish payload: ASCII paragraphs separated by blank lines, with
	// some collapsible whitespace runs sprinkled in so the relaxed path does
	// real work (not just a memcpy).
	var b bytes.Buffer
	for b.Len() < size {
		b.WriteString("Lorem    ipsum\tdolor sit amet,   consectetur adipiscing elit.   \r\n")
		b.WriteString("Sed   do eiusmod tempor incididunt   ut labore et\tdolore magna aliqua.\r\n")
		b.WriteString("\r\n")
	}
	return b.Bytes()[:size]
}

func BenchmarkCanonicalizeBodyRelaxed_4KB(b *testing.B) {
	body := benchBodyForCanon(4 * 1024)
	b.ResetTimer()
	b.SetBytes(int64(len(body)))
	for i := 0; i < b.N; i++ {
		_ = canonicalizeBodyRelaxed(body)
	}
}

func BenchmarkCanonicalizeBodyRelaxed_64KB(b *testing.B) {
	body := benchBodyForCanon(64 * 1024)
	b.ResetTimer()
	b.SetBytes(int64(len(body)))
	for i := 0; i < b.N; i++ {
		_ = canonicalizeBodyRelaxed(body)
	}
}

func BenchmarkCanonicalizeBodyRelaxed_1MB(b *testing.B) {
	body := benchBodyForCanon(1024 * 1024)
	b.ResetTimer()
	b.SetBytes(int64(len(body)))
	for i := 0; i < b.N; i++ {
		_ = canonicalizeBodyRelaxed(body)
	}
}

// BenchmarkCanonicalizeBodyRelaxed_OldImpl reproduces the previous
// strings.Split + regex implementation for comparison. Kept inline so the
// regression can be re-measured easily without git archaeology.
func BenchmarkCanonicalizeBodyRelaxed_OldImpl_64KB(b *testing.B) {
	body := benchBodyForCanon(64 * 1024)
	b.ResetTimer()
	b.SetBytes(int64(len(body)))
	for i := 0; i < b.N; i++ {
		_ = oldCanonicalizeBodyRelaxed(body)
	}
}

func oldCanonicalizeBodyRelaxed(body []byte) []byte {
	if len(body) == 0 {
		return []byte("\r\n")
	}
	var result strings.Builder
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		line = whitespaceRegex.ReplaceAllString(line, " ")
		line = strings.TrimRight(line, " \t")
		result.WriteString(line)
		if i < len(lines)-1 {
			result.WriteString("\r\n")
		}
	}
	s := result.String()
	if !strings.HasSuffix(s, "\r\n") {
		s += "\r\n"
	}
	for strings.HasSuffix(s, "\r\n\r\n") {
		s = s[:len(s)-2]
	}
	return []byte(s)
}

// TestCanonicalizeBodyRelaxed_MatchesOldImpl runs the new implementation
// against the preserved old one across a wide input space and asserts byte
// equality. This is the strongest backstop against silent regression in
// signature verification.
func TestCanonicalizeBodyRelaxed_MatchesOldImpl(t *testing.T) {
	cases := [][]byte{
		[]byte(""),
		[]byte("hello"),
		[]byte("hello\r\n"),
		[]byte("hello\r\n\r\n"),
		[]byte("hello\r\n\r\n\r\n"),
		[]byte("hello   world\r\n"),
		[]byte("\thello\r\n"),
		[]byte("hello\t\r\n"),
		[]byte("a\nb\nc\n"),
		[]byte("a\r\nb\r\nc\r\n"),
		[]byte("   \r\n"),
		[]byte("\r\n\r\n\r\n"),
		[]byte("line1   with    spaces\r\nline2\twith\ttabs\r\n"),
		[]byte("trailing\t \t\r\n"),
		benchBodyForCanon(4096),
		benchBodyForCanon(16 * 1024),
	}
	for i, c := range cases {
		got := canonicalizeBodyRelaxed(c)
		want := oldCanonicalizeBodyRelaxed(c)
		if !bytes.Equal(got, want) {
			t.Errorf("case %d: input=%q\n new = %q\n old = %q", i, string(c), string(got), string(want))
		}
	}
}
