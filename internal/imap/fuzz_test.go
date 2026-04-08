package imap

import (
	"testing"
)

// Fuzz ParseSequenceSet to find edge cases and panics
func FuzzParseSequenceSet(f *testing.F) {
	// Seed corpus with various sequence sets
	sequences := []string{
		"1",
		"1,2,3",
		"1:5",
		"1:5,7:10",
		"*",
		"1:*",
		"*:*",
		"",
		"*:*",
		"10:1", // reversed range
	}
	for _, s := range sequences {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, seq string) {
		_, err := ParseSequenceSet(seq)
		// Just verify no panic - errors are expected for invalid input
		_ = err
	})
}

// Fuzz parseFetchItems to find edge cases
func FuzzParseFetchItems(f *testing.F) {
	// Seed corpus with various fetch items
	items := []string{
		"FLAGS",
		"UID RFC822.SIZE",
		"BODY[TEXT]",
		"ENVELOPE",
		"(FLAGS UID)",
		"",
	}
	for _, s := range items {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, items string) {
		_ = parseFetchItems([]string{items})
	})
}
