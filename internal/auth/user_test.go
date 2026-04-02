package auth

import (
	"strings"
	"testing"
)

func TestGenerateCRAMMD5Challenge(t *testing.T) {
	challenge, challengeB64, err := GenerateCRAMMD5Challenge()
	if err != nil {
		t.Fatalf("GenerateCRAMMD5Challenge failed: %v", err)
	}

	// Challenge should be non-empty
	if challenge == "" {
		t.Error("Expected non-empty challenge")
	}

	// Challenge should start with < and end with @umailserver>
	if !strings.HasPrefix(challenge, "<") {
		t.Error("Expected challenge to start with <")
	}
	if !strings.HasSuffix(challenge, "@umailserver>") {
		t.Error("Expected challenge to end with @umailserver>")
	}

	// Base64 version should be non-empty
	if challengeB64 == "" {
		t.Error("Expected non-empty base64 challenge")
	}
}
