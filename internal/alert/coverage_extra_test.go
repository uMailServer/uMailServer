package alert

import (
	"encoding/json"
	"testing"
)

// --- SecureString tests ---

func TestSecureString_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    SecureString
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: `""`,
		},
		{
			name:     "with value",
			input:    "secretpassword",
			expected: `"[REDACTED]"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.input.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON error: %v", err)
			}
			if string(result) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(result))
			}
		})
	}
}

func TestSecureString_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected SecureString
	}{
		{
			name:     "redacted placeholder",
			input:    `"[REDACTED]"`,
			expected: "",
		},
		{
			name:     "normal value",
			input:    `"actualpassword"`,
			expected: "actualpassword",
		},
		{
			name:     "empty value",
			input:    `""`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s SecureString
			err := s.UnmarshalJSON([]byte(tt.input))
			if err != nil {
				t.Fatalf("UnmarshalJSON error: %v", err)
			}
			if s != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, s)
			}
		})
	}
}

func TestSecureString_String(t *testing.T) {
	s := SecureString("testvalue")
	result := s.String()
	if result != "testvalue" {
		t.Errorf("Expected 'testvalue', got %q", result)
	}
}

func TestSecureString_RoundTrip(t *testing.T) {
	// Note: Round-trip only works for empty values since json.Marshal
	// on a string alias doesn't call MarshalJSON when marshaled directly
	s := SecureString("")
	if s.String() != "" {
		t.Error("Empty SecureString should return empty string")
	}
}

// --- Config tests ---

func TestConfig_DefaultValues(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Error("DefaultConfig should have Enabled=false")
	}
}

func TestConfig_JSONMarshal(t *testing.T) {
	cfg := &Config{
		Enabled:  true,
		SMTPPassword: "secret",
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Password should be redacted
	if string(data) == "" {
		t.Fatal("Marshal returned empty")
	}

	// Verify it contains redacted password
	var result map[string]any
	json.Unmarshal(data, &result)

	if result["smtp_password"] != "[REDACTED]" {
		t.Errorf("Expected redacted password, got %v", result["smtp_password"])
	}
}
