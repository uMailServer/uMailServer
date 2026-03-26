package imap

import (
	"testing"
)

func BenchmarkParseSequenceSet(b *testing.B) {
	sets := []string{
		"1",
		"1:10",
		"1,3,5",
		"1:5,10:15,20",
		"*",
		"1:*",
	}

	for i := 0; i < b.N; i++ {
		for _, set := range sets {
			_, _ = ParseSequenceSet(set)
		}
	}
}

func BenchmarkParserAtom(b *testing.B) {
	inputs := []string{
		"INBOX",
		"Sent",
		"Drafts",
		"Trash",
	}

	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			p := NewParser(input)
			_ = p.Atom()
		}
	}
}

func BenchmarkParserNumber(b *testing.B) {
	inputs := []string{
		"123",
		"999999",
		"1",
	}

	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			p := NewParser(input)
			_ = p.Number()
		}
	}
}

func BenchmarkParseSeqNumber(b *testing.B) {
	inputs := []string{
		"1",
		"999999",
		"*",
	}

	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			_, _ = parseSeqNumber(input)
		}
	}
}
