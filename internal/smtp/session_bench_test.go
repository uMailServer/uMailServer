package smtp

import (
	"strings"
	"testing"
)

// BenchmarkParseAddress measures email address parsing performance
func BenchmarkParseAddress(b *testing.B) {
	addresses := []string{
		"<user@example.com>",
		"User Name <user@example.com>",
		"\"Quoted Name\" <user@example.com>",
		"user@example.com",
		"<user+tag@example.com>",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		addr := addresses[i%len(addresses)]
		parseAddress(addr)
	}
}

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

// BenchmarkExtractSender measures sender extraction from message
func BenchmarkExtractSender(b *testing.B) {
	msg := []byte("Return-Path: <sender@example.com>\r\n" +
		"From: Sender Name <sender@example.com>\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Test\r\n" +
		"\r\n" +
		"Body")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractSender(msg)
	}
}

// BenchmarkExtractRecipients measures recipient extraction
func BenchmarkExtractRecipients(b *testing.B) {
	msg := []byte("From: sender@example.com\r\n" +
		"To: recipient1@example.com, recipient2@example.com\r\n" +
		"Cc: cc1@example.com, cc2@example.com\r\n" +
		"Subject: Test\r\n" +
		"\r\n" +
		"Body")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractRecipients(msg)
	}
}

// Helper that mirrors actual implementation for consistent benchmarking
func parseAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if idx := strings.LastIndex(addr, "<"); idx != -1 {
		if endIdx := strings.Index(addr[idx:], ">"); endIdx != -1 {
			return addr[idx+1 : idx+endIdx]
		}
	}
	return addr
}

func extractSender(data []byte) string {
	// Look for Return-Path first
	if idx := strings.Index(string(data), "Return-Path:"); idx != -1 {
		start := idx + len("Return-Path:")
		remaining := string(data[start:])
		if end := strings.Index(remaining, "\r\n"); end != -1 {
			return strings.Trim(remaining[:end], " <>\t")
		}
	}

	// Fall back to From header
	if idx := strings.Index(string(data), "From:"); idx != -1 {
		start := idx + len("From:")
		remaining := string(data[start:])
		if end := strings.Index(remaining, "\r\n"); end != -1 {
			return parseAddress(remaining[:end])
		}
	}

	return ""
}

func extractRecipients(data []byte) []string {
	var recipients []string

	for _, header := range []string{"To:", "Cc:", "Bcc:"} {
		if idx := strings.Index(string(data), header); idx != -1 {
			start := idx + len(header)
			remaining := string(data[start:])
			if end := strings.Index(remaining, "\r\n"); end != -1 {
				value := remaining[:end]
				// Split by comma
				parts := strings.Split(value, ",")
				for _, part := range parts {
					recipients = append(recipients, parseAddress(strings.TrimSpace(part)))
				}
			}
		}
	}

	return recipients
}
