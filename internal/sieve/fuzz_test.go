package sieve

import (
	"testing"
)

// Fuzz parser to find edge cases and panics
func FuzzParser(f *testing.F) {
	// Seed corpus with valid and invalid scripts
	scripts := []string{
		"keep;",
		"discard;",
		`if header :contains "subject" "test" { keep; }`,
		`vacation "I'm on vacation";`,
		`reject "message";`,
		`fileinto "Trash";`,
		`redirect "test@example.com";`,
		`set "varname" "value";`,
		`stop;`,
	}
	for _, s := range scripts {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, script string) {
		p := NewParser(script)
		_, err := p.Parse()
		// Just verify no panic - parse errors are expected for invalid input
		_ = err
	})
}

// Fuzz interpreter Execute to find edge cases
func FuzzInterpreterExecute(f *testing.F) {
	// Seed corpus: script, from, to, body
	seedCorpus := [][4]string{
		{"keep;", "sender@test.com", "recipient@test.com", "Hello"},
		{"discard;", "sender@test.com", "recipient@test.com", "Body"},
		{`if header :contains "subject" "test" { keep; }`, "sender@test.com", "recipient@test.com", "Test message"},
	}
	for _, s := range seedCorpus {
		f.Add(s[0], s[1], s[2], s[3])
	}

	f.Fuzz(func(t *testing.T, script string, from string, to string, body string) {
		p := NewParser(script)
		s, err := p.Parse()
		if err != nil {
			return // Skip invalid scripts
		}

		interp := NewInterpreter(s)
		msg := &MessageContext{
			From:    from,
			To:      []string{to},
			Headers: map[string][]string{},
			Body:    []byte(body),
		}

		_, _ = interp.Execute(msg)
	})
}
