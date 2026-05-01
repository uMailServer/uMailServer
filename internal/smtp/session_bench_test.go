package smtp

import (
	"testing"
)

// BenchmarkParseCommand measures SMTP command parsing
func BenchmarkParseCommand(b *testing.B) {
	commands := []string{
		"EHLO example.com",
		"MAIL FROM:<sender@example.com>",
		"RCPT TO:<recipient@example.com>",
		"DATA",
		"QUIT",
		"RSET",
		"NOOP",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmd := commands[i%len(commands)]
		parseCommand(cmd)
	}
}

// BenchmarkParseMailFrom measures MAIL FROM parsing
func BenchmarkParseMailFrom(b *testing.B) {
	args := []string{
		"FROM:<sender@example.com>",
		"FROM:<sender@example.com> SIZE=1024",
		"from:<sender+tag@example.com>",
		"FROM:<>",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		arg := args[i%len(args)]
		parseMailFrom(arg)
	}
}

// BenchmarkParseRcptTo measures RCPT TO parsing
func BenchmarkParseRcptTo(b *testing.B) {
	args := []string{
		"TO:<recipient@example.com>",
		"to:<user+tag@example.com>",
		"TO:<recipient@example.com> ORCPT=rfc822;recipient@example.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		arg := args[i%len(args)]
		parseRcptTo(arg)
	}
}

// BenchmarkValidateEmail measures email validation performance
func BenchmarkValidateEmail(b *testing.B) {
	emails := []string{
		"user@example.com",
		"user.name@example.com",
		"user+tag@example.com",
		"user_name@sub.example.co.uk",
		"first.last@example-domain.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateEmail(emails[i%len(emails)])
	}
}
