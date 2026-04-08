package spam

import (
	"testing"
)

// Fuzz tokenizer to find edge cases and panics
func FuzzTokenizer(f *testing.F) {
	// Seed corpus
	texts := []string{
		"Hello world this is a test email",
		"URGENT: Click here now to claim your prize!!!",
		"Short",
		"",
	}
	for _, t := range texts {
		f.Add(t)
	}

	f.Fuzz(func(t *testing.T, text string) {
		tokenizer := NewTokenizer()
		_ = tokenizer.Tokenize(text)
	})
}

// Fuzz ExtractTokensFromHeaders to find edge cases
func FuzzExtractTokensFromHeaders(f *testing.F) {
	// Seed corpus: headerName, headerValue
	inputs := [][2]string{
		{"Subject", "Test Email"},
		{"From", "sender@example.com"},
		{"Content-Type", "text/html; charset=utf-8"},
	}
	for _, s := range inputs {
		f.Add(s[0], s[1])
	}

	f.Fuzz(func(t *testing.T, headerName string, headerValue string) {
		headers := map[string][]string{
			headerName: {headerValue},
		}
		_ = ExtractTokensFromHeaders(headers)
	})
}

// Fuzz extractEmails to find edge cases
func FuzzExtractEmails(f *testing.F) {
	// Seed corpus
	emails := []string{
		"user@example.com",
		"<user@example.com>",
		"",
		"invalid",
		"user@",
		"@domain.com",
	}
	for _, e := range emails {
		f.Add(e)
	}

	f.Fuzz(func(t *testing.T, text string) {
		_ = extractEmails(text)
	})
}
