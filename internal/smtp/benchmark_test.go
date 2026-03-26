package smtp

import (
	"bytes"
	"testing"
)

func BenchmarkCommandParse(b *testing.B) {
	cmds := [][]byte{
		[]byte("HELO example.com\r\n"),
		[]byte("MAIL FROM:<sender@example.com>\r\n"),
		[]byte("RCPT TO:<recipient@example.com>\r\n"),
		[]byte("DATA\r\n"),
		[]byte("QUIT\r\n"),
	}

	for i := 0; i < b.N; i++ {
		for _, cmd := range cmds {
			_, _ = parseLine(bytes.NewReader(cmd))
		}
	}
}

func BenchmarkParseAddress(b *testing.B) {
	addrs := []string{
		"<user@example.com>",
		`"John Doe" <john.doe@example.com>`,
		"user@example.com",
	}

	for i := 0; i < b.N; i++ {
		for _, addr := range addrs {
			_, _ = parseAddress(addr)
		}
	}
}

func BenchmarkNormalizeDomain(b *testing.B) {
	domains := []string{
		"example.com",
		"EXAMPLE.COM",
		"Sub.Example.COM",
	}

	for i := 0; i < b.N; i++ {
		for _, domain := range domains {
			_ = normalizeDomain(domain)
		}
	}
}
