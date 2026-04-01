package queue

import (
	"testing"
)

func TestEncodeVERP(t *testing.T) {
	tests := []struct {
		senderDomain string
		recipient    string
		expected     string
	}{
		{"mail.example.com", "john@example.com", "bounce-john=example.com@mail.example.com"},
		{"mail.test.org", "user@domain.org", "bounce-user=domain.org@mail.test.org"},
		{"mx.net", "a+b@c.d", "bounce-a+b=c.d@mx.net"},
	}

	for _, tt := range tests {
		result := EncodeVERP(tt.senderDomain, tt.recipient)
		if result != tt.expected {
			t.Errorf("EncodeVERP(%q, %q) = %q, want %q", tt.senderDomain, tt.recipient, result, tt.expected)
		}
	}
}

func TestDecodeVERP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"bounce-john=example.com@mail.example.com", "john@example.com"},
		{"bounce-user=domain.org@mail.test.org", "user@domain.org"},
		{"bounce-a+b=c.d@mx.net", "a+b@c.d"},
		{"bounce-UPPER=EXAMPLE.COM@mx.test", "UPPER@EXAMPLE.COM"},
		{"Bounce-john=example.com@mail.example.com", "john@example.com"}, // case-insensitive prefix
		{"john@example.com", ""},                   // not a bounce address
		{"bounce-invalid@mail.example.com", ""},    // missing = sign
		{"bounce-user@domain.com@mail.com", ""},    // missing = in user part (has @)
		{"", ""},                                   // empty
	}

	for _, tt := range tests {
		result := DecodeVERP(tt.input)
		if result != tt.expected {
			t.Errorf("DecodeVERP(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}



func TestVERPRoundTrip(t *testing.T) {
	recipients := []string{
		"john@example.com",
		"user@domain.org",
		"test+tag@sub.domain.com",
	}

	senderDomain := "mail.server.com"
	for _, rcpt := range recipients {
		encoded := EncodeVERP(senderDomain, rcpt)
		decoded := DecodeVERP(encoded)
		if decoded != rcpt {
			t.Errorf("VERP round trip failed: %q -> %q -> %q", rcpt, encoded, decoded)
		}
	}
}
