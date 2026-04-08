package smtp

import (
	"testing"
)

// Fuzz parseCommand to find edge cases and panics
func FuzzParseCommand(f *testing.F) {
	// Seed corpus with various SMTP commands
	commands := []string{
		"HELO example.com",
		"EHLO example.com",
		"MAIL FROM:<test@example.com>",
		"RCPT TO:<test@example.com>",
		"DATA",
		"QUIT",
		"NOOP",
		"RSET",
		"HELP",
		"VRFY test@example.com",
		"AUTH LOGIN",
		"AUTH PLAIN AHVzZXIAdGVzdC5jb20=",
		"STARTTLS",
		"   ",
		"",
	}
	for _, c := range commands {
		f.Add(c)
	}

	f.Fuzz(func(t *testing.T, line string) {
		cmd, arg := parseCommand(line)
		// Verify no panic and reasonable output
		_ = cmd
		_ = arg
	})
}
