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

