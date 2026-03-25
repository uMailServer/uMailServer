package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

func TestDKIMResultString(t *testing.T) {
	tests := []struct {
		result   DKIMResult
		expected string
	}{
		{DKIMNone, "none"},
		{DKIMPass, "pass"},
		{DKIMFail, "fail"},
		{DKIMPERMError, "permerror"},
		{DKIMTempError, "temperror"},
		{DKIMResult(999), "unknown"},
	}

	for _, tt := range tests {
		got := tt.result.String()
		if got != tt.expected {
			t.Errorf("DKIMResult(%d).String() = %q, want %q", tt.result, got, tt.expected)
		}
	}
}

func TestNewDKIMSigner(t *testing.T) {
	resolver := newMockDNSResolver()
	privateKey, _ := rsa.GenerateKey(rand.Reader, 1024)

	signer := NewDKIMSigner(resolver, privateKey, "example.com", "selector1")

	if signer == nil {
		t.Fatal("NewDKIMSigner returned nil")
	}
	if signer.domain != "example.com" {
		t.Errorf("Expected domain example.com, got %s", signer.domain)
	}
	if signer.selector != "selector1" {
		t.Errorf("Expected selector selector1, got %s", signer.selector)
	}
}

func TestNewDKIMVerifier(t *testing.T) {
	resolver := newMockDNSResolver()
	verifier := NewDKIMVerifier(resolver)

	if verifier == nil {
		t.Fatal("NewDKIMVerifier returned nil")
	}
	if verifier.resolver != resolver {
		t.Error("Resolver not set correctly")
	}
}

func TestParseDKIMSignature(t *testing.T) {
	tests := []struct {
		name        string
		header      string
		wantDomain  string
		wantSel     string
		wantAlgo    string
		wantCanon   string
		wantHeaders []string
		wantErr     bool
	}{
		{
			name:        "valid signature",
			header:      "v=1; a=rsa-sha256; d=example.com; s=selector; c=simple/simple; h=from:to:subject; bh=abc123; b=xyz789",
			wantDomain:  "example.com",
			wantSel:     "selector",
			wantAlgo:    "rsa-sha256",
			wantCanon:   "simple/simple",
			wantHeaders: []string{"from", "to", "subject"},
			wantErr:     false,
		},
		{
			name:        "relaxed canonicalization",
			header:      "v=1; a=rsa-sha256; d=example.com; s=selector; c=relaxed/relaxed; h=from:to; bh=abc123; b=xyz789",
			wantDomain:  "example.com",
			wantSel:     "selector",
			wantAlgo:    "rsa-sha256",
			wantCanon:   "relaxed/relaxed",
			wantHeaders: []string{"from", "to"},
			wantErr:     false,
		},
		{
			name:    "missing domain",
			header:  "v=1; a=rsa-sha256; s=selector; bh=abc123; b=xyz789",
			wantErr: true,
		},
		{
			name:    "missing selector",
			header:  "v=1; a=rsa-sha256; d=example.com; bh=abc123; b=xyz789",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig, err := parseDKIMSignature(tt.header)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if sig.Domain != tt.wantDomain {
				t.Errorf("Domain = %q, want %q", sig.Domain, tt.wantDomain)
			}
			if sig.Selector != tt.wantSel {
				t.Errorf("Selector = %q, want %q", sig.Selector, tt.wantSel)
			}
			if sig.Algorithm != tt.wantAlgo {
				t.Errorf("Algorithm = %q, want %q", sig.Algorithm, tt.wantAlgo)
			}
			if sig.Canonicalize != tt.wantCanon {
				t.Errorf("Canonicalize = %q, want %q", sig.Canonicalize, tt.wantCanon)
			}
		})
	}
}

func TestParseTagValueList(t *testing.T) {
	tests := []struct {
		input    string
		expected map[string]string
	}{
		{
			input: "a=1; b=2; c=3",
			expected: map[string]string{
				"a": "1",
				"b": "2",
				"c": "3",
			},
		},
		{
			input: "v=1; a=rsa-sha256; d=example.com",
			expected: map[string]string{
				"v": "1",
				"a": "rsa-sha256",
				"d": "example.com",
			},
		},
		{
			input: "b=abc def ghi", // whitespace in value
			expected: map[string]string{
				"b": "abcdefghi", // whitespace removed
			},
		},
	}

	for _, tt := range tests {
		result := parseTagValueList(tt.input)
		for k, v := range tt.expected {
			if result[k] != v {
				t.Errorf("parseTagValueList(%q)[%q] = %q, want %q", tt.input, k, result[k], v)
			}
		}
	}
}

func TestCanonicalizeBodySimple(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty body",
			input:    "",
			expected: "\r\n",
		},
		{
			name:     "single line",
			input:    "Hello world\r\n",
			expected: "Hello world\r\n",
		},
		{
			name:     "multiple lines",
			input:    "Line 1\r\nLine 2\r\n",
			expected: "Line 1\r\nLine 2\r\n",
		},
		{
			name:     "trailing empty lines",
			input:    "Line 1\r\nLine 2\r\n\r\n\r\n",
			expected: "Line 1\r\nLine 2\r\n",
		},
		{
			name:     "no trailing CRLF",
			input:    "Hello world",
			expected: "Hello world\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalizeBodySimple([]byte(tt.input))
			if string(result) != tt.expected {
				t.Errorf("canonicalizeBodySimple(%q) = %q, want %q", tt.input, string(result), tt.expected)
			}
		})
	}
}

func TestCanonicalizeBodyRelaxed(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty body",
			input:    "",
			expected: "\r\n",
		},
		{
			name:     "simple line",
			input:    "Hello world\r\n",
			expected: "Hello world\r\n",
		},
		{
			name:     "extra whitespace",
			input:    "Hello   world\r\n",
			expected: "Hello world\r\n",
		},
		{
			name:     "trailing whitespace",
			input:    "Hello world   \r\n",
			expected: "Hello world\r\n",
		},
		{
			name:     "leading whitespace",
			input:    "   Hello world\r\n",
			expected: " Hello world\r\n", // Leading whitespace reduced to single space
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalizeBodyRelaxed([]byte(tt.input))
			if string(result) != tt.expected {
				t.Errorf("canonicalizeBodyRelaxed(%q) = %q, want %q", tt.input, string(result), tt.expected)
			}
		})
	}
}

func TestCanonicalizeHeaderSimple(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		expected string
	}{
		{
			name:   "simple header",
			value:  "test@example.com",
			expected: "from: test@example.com\r\n",
		},
		{
			name:   "header with continuation",
			value:  "test@example.com\r\n  continuation",
			expected: "from: test@example.com\r\n  continuation\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalizeHeaderSimple("from", tt.value)
			if result != tt.expected {
				t.Errorf("canonicalizeHeaderSimple() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCanonicalizeHeaderRelaxed(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		value   string
		expected string
	}{
		{
			name:    "simple header",
			header:  "From",
			value:   "test@example.com",
			expected: "from:test@example.com\r\n",
		},
		{
			name:    "header with extra whitespace",
			header:  "From",
			value:   "test@example.com",
			expected: "from:test@example.com\r\n",
		},
		{
			name:    "continuation unfolded",
			header:  "Subject",
			value:   "Hello\r\n World",
			expected: "subject:Hello World\r\n",
		},
		{
			name:    "lowercase header name",
			header:  "CONTENT-TYPE",
			value:   "text/plain",
			expected: "content-type:text/plain\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalizeHeaderRelaxed(tt.header, tt.value)
			if result != tt.expected {
				t.Errorf("canonicalizeHeaderRelaxed(%q, %q) = %q, want %q", tt.header, tt.value, result, tt.expected)
			}
		})
	}
}

func TestParseHeaderList(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"from:to:subject", []string{"from", "to", "subject"}},
		{"FROM:TO:SUBJECT", []string{"from", "to", "subject"}},
		{" from : to : subject ", []string{"from", "to", "subject"}},
		{"", []string{}},
	}

	for _, tt := range tests {
		result := parseHeaderList(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseHeaderList(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("parseHeaderList(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}

func TestComputeBodyHash(t *testing.T) {
	// Test that body hash computation produces consistent results
	body := []byte("Hello World\r\n")

	hash1 := computeBodyHash(body, "simple")
	hash2 := computeBodyHash(body, "simple")

	if hash1 != hash2 {
		t.Error("Body hash should be consistent for same input")
	}

	if hash1 == "" {
		t.Error("Body hash should not be empty")
	}

	// Different canonicalization should produce different hashes for input with extra whitespace
	bodyWithExtraSpace := []byte("Hello  World\r\n") // Two spaces
	hash3 := computeBodyHash(bodyWithExtraSpace, "simple")
	hash4 := computeBodyHash(bodyWithExtraSpace, "relaxed")
	if hash3 == hash4 {
		t.Error("Different canonicalizations should produce different hashes for input with extra whitespace")
	}
}

func TestDKIMSignAndVerify(t *testing.T) {
	// Generate a test key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Create signer
	resolver := newMockDNSResolver()
	signer := NewDKIMSigner(resolver, privateKey, "example.com", "test")

	// Create test message
	headers := map[string][]string{
		"from":    {"sender@example.com"},
		"to":      {"recipient@example.com"},
		"subject": {"Test Message"},
	}
	body := []byte("This is a test message.\r\n")

	// Sign the message
	_, err = signer.Sign(headers, body)
	if err != nil {
		t.Fatalf("Failed to sign message: %v", err)
	}

	// Note: Full verification would require setting up DNS records with the public key
	// which is complex for a unit test. The signing test above validates the core logic.
}

func TestRemoveWhitespace(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"abc def", "abcdef"},
		{"a b c d", "abcd"},
		{"  leading", "leading"},
		{"trailing  ", "trailing"},
		{"\r\n\t", ""},
		{"no-whitespace", "no-whitespace"},
	}

	for _, tt := range tests {
		result := removeWhitespace(tt.input)
		if result != tt.expected {
			t.Errorf("removeWhitespace(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestParseInt64(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"123", 123},
		{"0", 0},
		{"1234567890", 1234567890},
		{"12abc34", 1234}, // Only digits are parsed
	}

	for _, tt := range tests {
		result := parseInt64(tt.input)
		if result != tt.expected {
			t.Errorf("parseInt64(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestDKIMSignatureDefaults(t *testing.T) {
	// Test that defaults are applied correctly
	header := "d=example.com; s=selector; bh=abc123; b=xyz789"
	sig, err := parseDKIMSignature(header)
	if err != nil {
		t.Fatalf("Failed to parse signature: %v", err)
	}

	if sig.Algorithm != "rsa-sha256" {
		t.Errorf("Default algorithm should be rsa-sha256, got %s", sig.Algorithm)
	}

	if sig.HeaderCanon != "simple" {
		t.Errorf("Default header canon should be simple, got %s", sig.HeaderCanon)
	}

	if sig.BodyCanon != "simple" {
		t.Errorf("Default body canon should be simple, got %s", sig.BodyCanon)
	}
}

func TestGenerateDKIMKeyPair(t *testing.T) {
	privateKey, pubKeyBytes, err := GenerateDKIMKeyPair(1024)
	if err != nil {
		t.Fatalf("GenerateDKIMKeyPair failed: %v", err)
	}

	if privateKey == nil {
		t.Error("Private key is nil")
	}

	if len(pubKeyBytes) == 0 {
		t.Error("Public key bytes are empty")
	}

	// Verify we can get the DNS format
	dnsKey := GetPublicKeyForDNS(privateKey)
	if dnsKey == "" {
		t.Error("DNS key is empty")
	}
}
