package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"strings"
	"testing"
	"time"
)

// =======================================================================
// DKIM Verify (41.7%) - exercise more paths through the Verify function.
//
// The Verify function calls fetchPublicKey which uses net.LookupTXT directly
// (not the mock resolver). Since we cannot mock DNS, we test the individual
// code paths that Verify uses internally.
// =======================================================================

// TestDKIMVerify_ParseErrorPath exercises the parse error path in Verify.
func TestDKIMVerify_ParseErrorPath_Cov5(t *testing.T) {
	resolver := newMockDNSResolver()
	verifier := NewDKIMVerifier(resolver)

	headers := map[string][]string{"From": {"test@example.com"}}

	result, sig, err := verifier.Verify(headers, []byte("body"), "")
	if result != DKIMFail {
		t.Errorf("Expected DKIMFail for empty header, got %v", result)
	}
	if sig != nil {
		t.Error("Expected nil sig for parse error")
	}
	if err == nil {
		t.Error("Expected error")
	}
}

// TestDKIMVerify_MissingDomainTag exercises the missing domain tag path.
func TestDKIMVerify_MissingDomainTag_Cov5(t *testing.T) {
	resolver := newMockDNSResolver()
	verifier := NewDKIMVerifier(resolver)

	headers := map[string][]string{"From": {"test@example.com"}}
	dkimHeader := "v=1; a=rsa-sha256; s=selector; bh=Y2FmZmVl; b=c3lnbmF0dXJl"

	result, _, err := verifier.Verify(headers, []byte("body"), dkimHeader)
	if result != DKIMFail {
		t.Errorf("Expected DKIMFail, got %v", result)
	}
	if err == nil {
		t.Error("Expected error for missing domain")
	}
}

// TestDKIMVerify_MissingSelectorTag exercises the missing selector tag path.
func TestDKIMVerify_MissingSelectorTag_Cov5(t *testing.T) {
	resolver := newMockDNSResolver()
	verifier := NewDKIMVerifier(resolver)

	headers := map[string][]string{"From": {"test@example.com"}}
	dkimHeader := "v=1; a=rsa-sha256; d=example.com; bh=Y2FmZmVl; b=c3lnbmF0dXJl"

	result, _, err := verifier.Verify(headers, []byte("body"), dkimHeader)
	if result != DKIMFail {
		t.Errorf("Expected DKIMFail, got %v", result)
	}
	if err == nil {
		t.Error("Expected error for missing selector")
	}
}

// TestDKIMVerify_UnsupportedAlgorithm exercises the unsupported algorithm path.
func TestDKIMVerify_UnsupportedAlgorithm_Cov5(t *testing.T) {
	resolver := newMockDNSResolver()
	verifier := NewDKIMVerifier(resolver)

	headers := map[string][]string{"From": {"test@example.com"}}
	dkimHeader := "v=1; a=rsa-sha1; d=example.com; s=sel; bh=abc; b=xyz"

	result, sig, err := verifier.Verify(headers, []byte("body"), dkimHeader)
	if result != DKIMFail {
		t.Errorf("Expected DKIMFail, got %v", result)
	}
	if sig == nil {
		t.Fatal("Expected non-nil sig for unsupported algorithm")
	}
	if err == nil {
		t.Error("Expected error for unsupported algorithm")
	}
	if !strings.Contains(err.Error(), "unsupported algorithm") {
		t.Errorf("Expected 'unsupported algorithm', got: %v", err)
	}
}

// TestDKIMVerify_DNSFailure exercises the DNS lookup failure path in Verify.
func TestDKIMVerify_DNSFailure_Cov5(t *testing.T) {
	resolver := newMockDNSResolver()
	verifier := NewDKIMVerifier(resolver)

	headers := map[string][]string{"From": {"test@example.com"}}
	dkimHeader := "v=1; a=rsa-sha256; d=example.com; s=selector; bh=abc; b=xyz"

	result, sig, err := verifier.Verify(headers, []byte("body"), dkimHeader)
	if result != DKIMFail && result != DKIMTempError {
		t.Errorf("Expected DKIMFail or DKIMTempError, got %v", result)
	}
	if sig == nil {
		t.Fatal("Expected non-nil sig")
	}
	if err == nil {
		t.Error("Expected error for DNS failure")
	}
}

// TestDKIMVerify_BodyHashInternals exercises the body hash verification path
// that Verify uses internally, through direct function calls.
func TestDKIMVerify_BodyHashInternals_Cov5(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	resolver := newMockDNSResolver()
	signer := NewDKIMSigner(resolver, privateKey, "example.com", "test")

	headers := map[string][]string{
		"from":    {"sender@example.com"},
		"to":      {"recipient@example.com"},
		"subject": {"Test"},
	}
	body := []byte("This is a test message body.\r\n")

	dkimHeader, err := signer.Sign(headers, body)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	sig, err := parseDKIMSignature(dkimHeader)
	if err != nil {
		t.Fatalf("parseDKIMSignature: %v", err)
	}

	// Verify body hash matches (this is what Verify does internally)
	canonicalBody := canonicalizeBody(body, sig.BodyCanon)
	computedBodyHash := sha256Hash(canonicalBody)
	if computedBodyHash != sig.BodyHash {
		t.Errorf("Body hash mismatch: computed=%s sig=%s", computedBodyHash, sig.BodyHash)
	}
}

// TestDKIMVerify_SignatureInternals exercises the RSA signature verification
// path that Verify uses internally.
func TestDKIMVerify_SignatureInternals_Cov5(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	resolver := newMockDNSResolver()
	signer := NewDKIMSigner(resolver, privateKey, "example.com", "test")

	headers := map[string][]string{
		"from": {"sender@example.com"},
	}
	body := []byte("Test body.\r\n")

	dkimHeader, err := signer.Sign(headers, body)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	sig, _ := parseDKIMSignature(dkimHeader)

	// Reproduce what computeSignature does: canonicalizeHeaders + buildHeaderWithoutSig
	canonicalHeaders := canonicalizeHeaders(headers, sig.SignedHeaders, sig.HeaderCanon)
	partialHeader := signer.buildHeaderWithoutSig(sig)
	sigData := canonicalHeaders + partialHeader

	// This should verify correctly
	err = verifyRSASignature(&privateKey.PublicKey, []byte(sigData), sig.Signature)
	if err != nil {
		t.Errorf("RSA verification failed: %v", err)
	}

	// Wrong data should fail
	wrongSigData := canonicalHeaders + "v=1; b=\r\n"
	err = verifyRSASignature(&privateKey.PublicKey, []byte(wrongSigData), sig.Signature)
	if err == nil {
		t.Error("Expected verification failure with wrong data")
	}
}

// TestDKIMVerify_BodyHashMismatchInternal exercises the body hash mismatch path.
func TestDKIMVerify_BodyHashMismatchInternal_Cov5(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	resolver := newMockDNSResolver()
	signer := NewDKIMSigner(resolver, privateKey, "example.com", "test")

	headers := map[string][]string{"from": {"sender@example.com"}}
	originalBody := []byte("Original body.\r\n")

	dkimHeader, err := signer.Sign(headers, originalBody)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	sig, _ := parseDKIMSignature(dkimHeader)

	// Use a different body
	tamperedBody := []byte("Tampered body.\r\n")
	canonicalBody := canonicalizeBody(tamperedBody, sig.BodyCanon)
	computedBodyHash := sha256Hash(canonicalBody)
	if computedBodyHash == sig.BodyHash {
		t.Error("Body hash should not match with tampered body")
	}
}

// TestDKIMVerify_BodyLengthPath exercises the body length limit path.
func TestDKIMVerify_BodyLengthPath_Cov5(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	resolver := newMockDNSResolver()
	signer := NewDKIMSigner(resolver, privateKey, "example.com", "test")

	headers := map[string][]string{"from": {"sender@example.com"}}
	body := []byte("A longer body for testing truncation.\r\n")

	dkimHeader, err := signer.Sign(headers, body)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	sig, _ := parseDKIMSignature(dkimHeader)

	// Default BodyLength should be -1 (no truncation)
	if sig.BodyLength != -1 {
		t.Errorf("Expected BodyLength=-1, got %d", sig.BodyLength)
	}

	// When BodyLength is -1, no truncation should happen
	canonicalBody := canonicalizeBody(body, sig.BodyCanon)
	// Verify the body hash path with no truncation
	computedHash := sha256Hash(canonicalBody)
	if computedHash != sig.BodyHash {
		t.Errorf("Body hash should match without truncation")
	}

	// Manually set BodyLength to test truncation path
	sig.BodyLength = 10
	shortBody := canonicalizeBody(body, sig.BodyCanon)
	if len(shortBody) > sig.BodyLength {
		shortBody = shortBody[:sig.BodyLength]
	}
	truncatedHash := sha256Hash(shortBody)
	if truncatedHash == sig.BodyHash {
		t.Error("Truncated body hash should differ from full body hash")
	}

	// BodyLength larger than body - no truncation
	sig.BodyLength = 10000
	longBody := canonicalizeBody(body, sig.BodyCanon)
	if len(longBody) > sig.BodyLength {
		longBody = longBody[:sig.BodyLength]
	}
	longHash := sha256Hash(longBody)
	if longHash != sig.BodyHash {
		t.Error("Body hash should match when limit exceeds body length")
	}
}

// =======================================================================
// fetchPublicKey (77.8%) - tests through net.LookupTXT.
// The function uses real DNS, so we test the error path.
// =======================================================================

// TestDKIMFetchPublicKey_NonexistentDomain tests fetchPublicKey with a domain
// that won't have DKIM records.
func TestDKIMFetchPublicKey_NonexistentDomain_Cov5(t *testing.T) {
	resolver := newMockDNSResolver()
	verifier := NewDKIMVerifier(resolver)

	// This domain won't exist in DNS, so net.LookupTXT should fail
	_, err := verifier.fetchPublicKey("nonexistent.invalid", "selector")
	if err == nil {
		t.Error("Expected error for nonexistent domain")
	}
}

// =======================================================================
// GenerateTOTPSecret (75.0%) - exercise the happy path more thoroughly.
// =======================================================================

// TestGenerateTOTPSecret_Valid tests that GenerateTOTPSecret produces valid secrets.
func TestGenerateTOTPSecret_Valid_Cov5(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if secret == "" {
		t.Error("Expected non-empty secret")
	}
	// Should be base32 encoded (no padding)
	if strings.Contains(secret, "=") {
		t.Error("Expected no padding in base32 secret")
	}
	// Should be 32 characters (20 bytes * 8 bits / 5 bits per base32 char = 32)
	if len(secret) != 32 {
		t.Errorf("Expected 32 chars, got %d", len(secret))
	}
}

// TestGenerateTOTPSecret_Uniqueness tests that multiple secrets are unique.
func TestGenerateTOTPSecret_Uniqueness_Cov5(t *testing.T) {
	secrets := make(map[string]bool)
	for i := 0; i < 10; i++ {
		secret, err := GenerateTOTPSecret()
		if err != nil {
			t.Fatalf("GenerateTOTPSecret %d: %v", i, err)
		}
		if secrets[secret] {
			t.Errorf("Duplicate secret generated at iteration %d", i)
		}
		secrets[secret] = true
	}
}

// =======================================================================
// DKIM parseDKIMPublicKey - cover more paths.
// =======================================================================

// TestParseDKIMPublicKey_EmptyPValue tests the empty p= tag path.
func TestParseDKIMPublicKey_EmptyPValue_Cov5(t *testing.T) {
	_, err := parseDKIMPublicKey("v=DKIM1; k=rsa; p=")
	if err == nil {
		t.Error("Expected error for empty p= value")
	}
}

// TestParseDKIMPublicKey_Ed25519KeyType tests parsing with unsupported key type.
func TestParseDKIMPublicKey_Ed25519KeyType_Cov5(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pubKeyDNS := GetPublicKeyForDNS(privateKey)

	_, err = parseDKIMPublicKey("v=DKIM1; k=ed25519; p=" + pubKeyDNS)
	if err == nil {
		t.Error("Expected error for unsupported key type")
	}
}

// TestParseDKIMPublicKey_InvalidBase64 tests parsing with invalid base64 key data.
func TestParseDKIMPublicKey_InvalidBase64_Cov5(t *testing.T) {
	_, err := parseDKIMPublicKey("v=DKIM1; k=rsa; p=!!!invalid!!!")
	if err == nil {
		t.Error("Expected error for invalid base64")
	}
}

// TestParseDKIMPublicKey_UnsupportedVersion tests parsing with wrong version.
func TestParseDKIMPublicKey_UnsupportedVersion_Cov5(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pubKeyDNS := GetPublicKeyForDNS(privateKey)

	_, err = parseDKIMPublicKey("v=DKIM2; k=rsa; p=" + pubKeyDNS)
	if err == nil {
		t.Error("Expected error for unsupported version")
	}
}

// =======================================================================
// DKIM parseDKIMSignature - cover additional parse paths.
// =======================================================================

// TestDKIMParseSignature_Version2 tests parsing with version 2 (rejected).
func TestDKIMParseSignature_Version2_Cov5(t *testing.T) {
	_, err := parseDKIMSignature("v=2; d=example.com; s=selector; bh=abc; b=xyz")
	if err == nil {
		t.Error("Expected error for DKIM version 2")
	}
	if !strings.Contains(err.Error(), "unsupported DKIM version") {
		t.Errorf("Expected 'unsupported DKIM version' error, got: %v", err)
	}
}

// TestDKIMParseSignature_MissingBodyHash tests parsing with missing bh= tag.
func TestDKIMParseSignature_MissingBodyHash_Cov5(t *testing.T) {
	_, err := parseDKIMSignature("v=1; d=example.com; s=selector; b=xyz")
	if err == nil {
		t.Error("Expected error for missing body hash")
	}
}

// TestDKIMParseSignature_MissingSignature tests parsing with missing b= tag.
func TestDKIMParseSignature_MissingSignature_Cov5(t *testing.T) {
	_, err := parseDKIMSignature("v=1; d=example.com; s=selector; bh=abc")
	if err == nil {
		t.Error("Expected error for missing signature")
	}
}

// TestDKIMParseSignature_AllTags tests parsing with all possible tags.
func TestDKIMParseSignature_AllTags_Cov5(t *testing.T) {
	header := "v=1; a=rsa-sha256; c=relaxed/relaxed; d=example.com; s=mysel; q=dns/txt; t=1609459200; x=1609545600; h=from:to:subject; bh=Y2FmZmVl; b=c3lnbmF0dXJl; l=1234; z=from=sender@example.com|to=recv@example.com"

	sig, err := parseDKIMSignature(header)
	if err != nil {
		t.Fatalf("parseDKIMSignature: %v", err)
	}

	if sig.Algorithm != "rsa-sha256" {
		t.Errorf("Algorithm = %q, want rsa-sha256", sig.Algorithm)
	}
	if sig.HeaderCanon != "relaxed" {
		t.Errorf("HeaderCanon = %q, want relaxed", sig.HeaderCanon)
	}
	if sig.BodyCanon != "relaxed" {
		t.Errorf("BodyCanon = %q, want relaxed", sig.BodyCanon)
	}
	if sig.BodyLength != 1234 {
		t.Errorf("BodyLength = %d, want 1234", sig.BodyLength)
	}
	if sig.QueryMethod != "dns/txt" {
		t.Errorf("QueryMethod = %q, want dns/txt", sig.QueryMethod)
	}
	if sig.Timestamp != 1609459200 {
		t.Errorf("Timestamp = %d, want 1609459200", sig.Timestamp)
	}
	if sig.Expiration != 1609545600 {
		t.Errorf("Expiration = %d, want 1609545600", sig.Expiration)
	}
	if len(sig.CopiedHeaders) != 2 {
		t.Errorf("CopiedHeaders = %d entries, want 2", len(sig.CopiedHeaders))
	}
}

// =======================================================================
// MTA-STS GetPolicy - exercise caching paths more thoroughly.
// =======================================================================

// TestGetPolicy_WithCachedPolicy tests GetPolicy returns cached policy.
func TestGetPolicy_WithCachedPolicy_Cov5(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	validator.cacheMu.Lock()
	validator.cache["cached.com"] = &MTASTSCacheEntry{
		Policy: &MTASTSPolicy{
			Version: "STSv1",
			Mode:    MTASTSModeEnforce,
			MX:      []string{"mail.cached.com"},
			MaxAge:  86400,
		},
		Domain:    "cached.com",
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	validator.cacheMu.Unlock()

	policy, err := validator.GetPolicy(context.Background(), "cached.com")
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if policy == nil {
		t.Fatal("Expected non-nil policy")
	}
	if policy.Mode != MTASTSModeEnforce {
		t.Errorf("Mode = %q, want enforce", policy.Mode)
	}
}

// TestGetPolicy_WithExpiredCache tests GetPolicy with expired cache entry.
func TestGetPolicy_WithExpiredCache_Cov5(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	validator.cacheMu.Lock()
	validator.cache["expired.com"] = &MTASTSCacheEntry{
		Policy: &MTASTSPolicy{
			Version: "STSv1",
			Mode:    MTASTSModeTesting,
			MX:      []string{"mail.expired.com"},
			MaxAge:  86400,
		},
		Domain:    "expired.com",
		FetchedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	validator.cacheMu.Unlock()

	policy, err := validator.GetPolicy(context.Background(), "expired.com")
	if err != nil {
		t.Fatalf("GetPolicy should not error: %v", err)
	}
	if policy != nil {
		t.Error("Expected nil policy when no DNS record exists")
	}
}

// TestGetPolicy_NilPolicyCaching tests that nil policy results are cached.
func TestGetPolicy_NilPolicyCaching_Cov5(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	policy1, err := validator.GetPolicy(context.Background(), "nocache.com")
	if err != nil {
		t.Fatalf("First GetPolicy: %v", err)
	}
	if policy1 != nil {
		t.Error("Expected nil policy")
	}

	// Verify it was cached (negative cache)
	validator.cacheMu.RLock()
	entry, ok := validator.cache["nocache.com"]
	validator.cacheMu.RUnlock()
	if !ok {
		t.Error("Expected cache entry for nocache.com")
	}
	if entry != nil && entry.Policy != nil {
		t.Error("Expected nil policy in cache entry")
	}
}

// =======================================================================
// DKIM signRSA (80.0%) - additional paths.
// =======================================================================

// TestSignRSA_LargeData tests signRSA with a larger payload.
func TestSignRSA_LargeData_Cov5(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	largeData := make([]byte, 8192)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	sig, err := signRSA(privateKey, largeData)
	if err != nil {
		t.Fatalf("signRSA with large data: %v", err)
	}
	if sig == "" {
		t.Error("Expected non-empty signature")
	}

	err = verifyRSASignature(&privateKey.PublicKey, largeData, sig)
	if err != nil {
		t.Errorf("verifyRSASignature failed: %v", err)
	}
}

// TestSignRSA_EmptyData tests signRSA with empty data.
func TestSignRSA_EmptyData_Cov5(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	sig, err := signRSA(privateKey, []byte{})
	if err != nil {
		t.Fatalf("signRSA with empty data: %v", err)
	}
	if sig == "" {
		t.Error("Expected non-empty signature for empty data")
	}

	err = verifyRSASignature(&privateKey.PublicKey, []byte{}, sig)
	if err != nil {
		t.Errorf("verifyRSASignature failed for empty data: %v", err)
	}
}

// TestSignRSA_DifferentKeySizes tests signRSA with different key sizes.
func TestSignRSA_DifferentKeySizes_Cov5(t *testing.T) {
	for _, bits := range []int{2048, 4096} {
		t.Run(fmt.Sprintf("%d", bits), func(t *testing.T) {
			privateKey, err := rsa.GenerateKey(rand.Reader, bits)
			if err != nil {
				t.Fatalf("GenerateKey(%d): %v", bits, err)
			}

			data := []byte("test data to sign")
			sig, err := signRSA(privateKey, data)
			if err != nil {
				t.Fatalf("signRSA(%d): %v", bits, err)
			}

			err = verifyRSASignature(&privateKey.PublicKey, data, sig)
			if err != nil {
				t.Errorf("verifyRSASignature(%d) failed: %v", bits, err)
			}
		})
	}
}

// =======================================================================
// DKIM verifyRSASignature - additional edge cases.
// =======================================================================

// TestVerifyRSASignature_EmptySignature tests with an empty base64 signature.
func TestVerifyRSASignature_EmptySignature_Cov5(t *testing.T) {
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)

	err := verifyRSASignature(&privateKey.PublicKey, []byte("test"), base64EncodeEmpty())
	if err == nil {
		t.Error("Expected error for empty signature bytes")
	}
}

func base64EncodeEmpty() string {
	return ""
}

// TestVerifyRSASignature_CorruptedBase64 tests with invalid base64.
func TestVerifyRSASignature_CorruptedBase64_Cov5(t *testing.T) {
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)

	err := verifyRSASignature(&privateKey.PublicKey, []byte("test"), "not!valid!base64!!!")
	if err == nil {
		t.Error("Expected error for corrupted base64")
	}
}

// =======================================================================
// DKIM header canonicalization - additional paths.
// =======================================================================

// TestDKIMVerify_AllCanonicalizationCombos exercises all canonicalization
// combinations through signing and individual verification.
func TestDKIMVerify_AllCanonicalizationCombos_Cov5(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	combinations := []struct {
		headerCanon string
		bodyCanon   string
	}{
		{"simple", "simple"},
		{"simple", "relaxed"},
		{"relaxed", "simple"},
		{"relaxed", "relaxed"},
	}

	for _, combo := range combinations {
		t.Run(fmt.Sprintf("%s/%s", combo.headerCanon, combo.bodyCanon), func(t *testing.T) {
			resolver := newMockDNSResolver()
			signer := NewDKIMSigner(resolver, privateKey, "example.com", "test")

			headers := map[string][]string{
				"from":    {"sender@example.com"},
				"to":      {"recipient@example.com"},
				"subject": {"Test Message"},
			}
			body := []byte("Message body content.\r\n")

			// Construct a DKIMSignature with the desired canonicalization
			sig := &DKIMSignature{
				Domain:        "example.com",
				Selector:      "test",
				Algorithm:     "rsa-sha256",
				Canonicalize:  combo.headerCanon + "/" + combo.bodyCanon,
				HeaderCanon:   combo.headerCanon,
				BodyCanon:     combo.bodyCanon,
				SignedHeaders: []string{"from", "to", "subject"},
				BodyLength:    -1,
			}

			// Compute the signature using computeSignature
			signature, err := signer.computeSignature(headers, body, sig)
			if err != nil {
				t.Fatalf("computeSignature: %v", err)
			}
			sig.Signature = signature

			// Verify body hash
			canonicalBody := canonicalizeBody(body, sig.BodyCanon)
			computedHash := sha256Hash(canonicalBody)
			if computedHash != sig.BodyHash {
				t.Errorf("Body hash mismatch for %s/%s", combo.headerCanon, combo.bodyCanon)
			}
		})
	}
}

// =======================================================================
// Additional DKIM tests for dkimHeaderWithoutSig edge cases.
// =======================================================================

// TestDkimHeaderWithoutSig_EdgeCases tests dkimHeaderWithoutSig with various inputs.
func TestDkimHeaderWithoutSig_EdgeCases_Cov5(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "b at start",
			input:    "b=signature; d=example.com",
			expected: "b=; d=example.com",
		},
		{
			name:     "b in middle",
			input:    "v=1; b=sig123; d=example.com",
			expected: "v=1; b=; d=example.com",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no b= tag",
			input:    "v=1; d=example.com; s=selector",
			expected: "v=1; d=example.com; s=selector",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dkimHeaderWithoutSig(tt.input)
			if got != tt.expected {
				t.Errorf("dkimHeaderWithoutSig(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
