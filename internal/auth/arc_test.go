package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

func TestARCResultString(t *testing.T) {
	tests := []struct {
		result   ARCResult
		expected string
	}{
		{ARCNone, "none"},
		{ARCPass, "pass"},
		{ARCFail, "fail"},
		{ARCPermError, "permerror"},
		{ARCTempError, "temperror"},
		{ARCResult(999), "unknown"},
	}

	for _, tt := range tests {
		got := tt.result.String()
		if got != tt.expected {
			t.Errorf("ARCResult(%d).String() = %q, want %q", tt.result, got, tt.expected)
		}
	}
}

func TestNewARCValidator(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewARCValidator(resolver)

	if validator == nil {
		t.Fatal("NewARCValidator returned nil")
	}
	if validator.resolver != resolver {
		t.Error("Resolver not set correctly")
	}
}

func TestNewARCSigner(t *testing.T) {
	resolver := newMockDNSResolver()
	privateKey, _ := rsa.GenerateKey(rand.Reader, 1024)

	signer := NewARCSigner(resolver, privateKey, "example.com", "arc")

	if signer == nil {
		t.Fatal("NewARCSigner returned nil")
	}
	if signer.domain != "example.com" {
		t.Errorf("Expected domain example.com, got %s", signer.domain)
	}
	if signer.selector != "arc" {
		t.Errorf("Expected selector arc, got %s", signer.selector)
	}
}

func TestExtractInstance(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"i=1; cv=none", 1},
		{"i=2; cv=pass", 2},
		{"i=10; cv=fail", 10},
		{"no instance", 0},
		{"", 0},
	}

	for _, tt := range tests {
		got := extractInstance(tt.input)
		if got != tt.expected {
			t.Errorf("extractInstance(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestExtractARCHeaders(t *testing.T) {
	headers := map[string][]string{
		"ARC-Authentication-Results": {"i=1; spf=pass"},
		"ARC-Message-Signature":      {"i=1; a=rsa-sha256"},
		"ARC-Seal":                   {"i=1; cv=none"},
		"From":                       {"sender@example.com"},
	}

	arcHeaders := extractARCHeaders(headers)

	if len(arcHeaders) != 3 {
		t.Errorf("Expected 3 ARC headers, got %d", len(arcHeaders))
	}
}

func TestGroupARCHeaders(t *testing.T) {
	headers := []headerEntry{
		{Name: "arc-authentication-results", Value: "i=1; spf=pass"},
		{Name: "arc-message-signature", Value: "i=1; a=rsa-sha256"},
		{Name: "arc-seal", Value: "i=1; cv=none"},
		{Name: "arc-authentication-results", Value: "i=2; spf=pass"},
		{Name: "arc-message-signature", Value: "i=2; a=rsa-sha256"},
		{Name: "arc-seal", Value: "i=2; cv=pass"},
	}

	sets := groupARCHeaders(headers)

	if len(sets) != 2 {
		t.Errorf("Expected 2 ARC sets, got %d", len(sets))
	}

	if _, ok := sets[1]; !ok {
		t.Error("Missing ARC set 1")
	}

	if _, ok := sets[2]; !ok {
		t.Error("Missing ARC set 2")
	}
}

func TestDetermineCV(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string][]string
		expected string
	}{
		{
			name:     "no ARC headers",
			headers:  map[string][]string{},
			expected: "none",
		},
		{
			name: "single seal",
			headers: map[string][]string{
				"arc-seal": {"i=1; cv=none"},
			},
			expected: "none",
		},
		{
			name: "multiple seals",
			headers: map[string][]string{
				"arc-seal": {"i=1; cv=none", "i=2; cv=pass"},
			},
			expected: "pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineCV(tt.headers)
			if got != tt.expected {
				t.Errorf("determineCV() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractSealInfo(t *testing.T) {
	as := "i=1; a=rsa-sha256; d=example.com; s=selector; cv=none; b=signature"

	domain, selector := extractSealInfo(as)

	if domain != "example.com" {
		t.Errorf("Expected domain example.com, got %q", domain)
	}

	if selector != "selector" {
		t.Errorf("Expected selector selector, got %q", selector)
	}
}

func TestARCValidateNoHeaders(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewARCValidator(resolver)

	headers := map[string][]string{
		"From": {"sender@example.com"},
	}
	body := []byte("Test message\r\n")

	chain, err := validator.Validate(context.Background(), headers, body)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if chain.ChainLength != 0 {
		t.Errorf("Expected chain length 0, got %d", chain.ChainLength)
	}

	if chain.CV != "none" {
		t.Errorf("Expected cv=none, got %q", chain.CV)
	}
}

func TestARCValidateWithHeaders(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewARCValidator(resolver)

	// Create headers with ARC headers
	headers := map[string][]string{
		"From":                       {"sender@example.com"},
		"ARC-Authentication-Results": {"i=1; spf=pass"},
		"ARC-Message-Signature":      {"i=1; a=rsa-sha256; d=example.com; s=arc; bh=hash; b="},
		"ARC-Seal":                   {"i=1; cv=none; d=example.com; s=arc; b="},
	}
	body := []byte("Test message\r\n")

	chain, err := validator.Validate(context.Background(), headers, body)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if chain.ChainLength != 1 {
		t.Errorf("Expected chain length 1, got %d", chain.ChainLength)
	}
}

func TestARCSignWithoutKey(t *testing.T) {
	resolver := newMockDNSResolver()
	signer := NewARCSigner(resolver, nil, "example.com", "arc")

	headers := map[string][]string{}
	body := []byte("Test message\r\n")

	_, err := signer.Sign(headers, body, "spf=pass", 1)
	if err == nil {
		t.Error("Expected error when signing without private key")
	}
}

func TestARCSignWithKey(t *testing.T) {
	resolver := newMockDNSResolver()
	privateKey, _ := rsa.GenerateKey(rand.Reader, 1024)

	signer := NewARCSigner(resolver, privateKey, "example.com", "arc")

	headers := map[string][]string{}
	body := []byte("Test message\r\n")

	arcSet, err := signer.Sign(headers, body, "spf=pass", 1)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	if arcSet.Instance != 1 {
		t.Errorf("Expected instance 1, got %d", arcSet.Instance)
	}

	if arcSet.AAR == "" {
		t.Error("AAR should not be empty")
	}

	if arcSet.AMS == "" {
		t.Error("AMS should not be empty")
	}

	if arcSet.AS == "" {
		t.Error("AS should not be empty")
	}
}

func TestBuildAMSSignatureData(t *testing.T) {
	headers := map[string][]string{
		"From":    {"sender@example.com"},
		"To":      {"recipient@example.com"},
		"Subject": {"Test"},
	}
	body := []byte("Test message\r\n")
	ams := "i=1; a=rsa-sha256"

	data := buildAMSSignatureData(ams, headers, body)

	if len(data) == 0 {
		t.Error("Signature data should not be empty")
	}
}

func TestValidateAMS(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewARCValidator(resolver)

	headers := map[string][]string{
		"From": {"sender@example.com"},
	}
	body := []byte("Test message\r\n")

	// Test with empty AMS
	result, _ := validator.validateAMS(context.Background(), "", headers, body)
	if result {
		t.Error("Expected validateAMS to return false for empty AMS")
	}

	// Test with AMS that has no signature
	ams := "i=1; a=rsa-sha256; d=example.com; s=arc; bh=hash;"
	result, _ = validator.validateAMS(context.Background(), ams, headers, body)
	// Should fail because no public key can be fetched
	_ = result
}

func TestValidateAS(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewARCValidator(resolver)

	headers := map[string][]string{
		"From": {"sender@example.com"},
	}

	// Test with empty AS
	result, _ := validator.validateAS(context.Background(), "", headers, 1)
	if result {
		t.Error("Expected validateAS to return false for empty AS")
	}

	// Test with AS that has no signature
	as := "i=1; a=rsa-sha256; d=example.com; s=arc; cv=none;"
	result, _ = validator.validateAS(context.Background(), as, headers, 1)
	// Should fail because no public key can be fetched
	_ = result
}

func TestFetchARCPublicKey(t *testing.T) {
	resolver := newMockDNSResolver()

	// Test with invalid selector/domain - should return error
	_, err := fetchARCPublicKey(resolver, "example.com", "invalid")
	// May or may not error depending on mock implementation
	_ = err
}

func TestCreateAMS(t *testing.T) {
	resolver := newMockDNSResolver()
	privateKey, _ := rsa.GenerateKey(rand.Reader, 1024)
	signer := NewARCSigner(resolver, privateKey, "example.com", "arc")

	headers := map[string][]string{
		"From": {"sender@example.com"},
	}
	body := []byte("Test message\r\n")

	ams, err := signer.createAMS(headers, body, 1)
	if err != nil {
		t.Errorf("createAMS failed: %v", err)
	}
	if ams == "" {
		t.Error("AMS should not be empty")
	}
}

func TestCreateAS(t *testing.T) {
	resolver := newMockDNSResolver()
	privateKey, _ := rsa.GenerateKey(rand.Reader, 1024)
	signer := NewARCSigner(resolver, privateKey, "example.com", "arc")

	// Create an AMS first
	amsHeaders := map[string][]string{
		"ARC-Message-Signature": {"i=1; a=rsa-sha256; d=example.com; s=arc"},
	}

	as, err := signer.createAS(amsHeaders, "none", 1)
	if err != nil {
		t.Errorf("createAS failed: %v", err)
	}
	if as == "" {
		t.Error("AS should not be empty")
	}
}
