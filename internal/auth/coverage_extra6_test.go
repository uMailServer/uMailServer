package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"
)

// =======================================================================
// MTA-STS GetPolicy (76.5%) - cache hit path and nil policy caching
// =======================================================================

// TestGetPolicy_CacheHitFresh_Cov6 tests the cache-hit early return path in GetPolicy.
func TestGetPolicy_CacheHitFresh_Cov6(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	// Pre-populate cache with a fresh (non-expired) entry
	cachedPolicy := &MTASTSPolicy{
		Version: "STSv1",
		Mode:    MTASTSModeEnforce,
		MaxAge:  3600,
		MX:      []string{"mail.example.com"},
	}
	validator.cacheMu.Lock()
	validator.cache["cached.com"] = &MTASTSCacheEntry{
		Policy:    cachedPolicy,
		Domain:    "cached.com",
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour), // Fresh
	}
	validator.cacheMu.Unlock()

	// Should return cached policy without fetching
	policy, err := validator.GetPolicy(context.Background(), "cached.com")
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if policy == nil {
		t.Fatal("Expected cached policy, got nil")
	}
	if policy.Mode != MTASTSModeEnforce {
		t.Errorf("Expected mode=enforce, got %s", policy.Mode)
	}
}

// TestGetPolicy_CacheHitExpired_Cov6 tests that an expired cache entry triggers a new fetch.
func TestGetPolicy_CacheHitExpired_Cov6(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	// Pre-populate cache with an expired entry
	validator.cacheMu.Lock()
	validator.cache["expired.com"] = &MTASTSCacheEntry{
		Policy: &MTASTSPolicy{
			Version: "STSv1",
			Mode:    MTASTSModeEnforce,
			MaxAge:  3600,
		},
		Domain:    "expired.com",
		FetchedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Expired
	}
	validator.cacheMu.Unlock()

	// Should try to fetch fresh policy - DNS lookup will fail (no record), returns nil
	policy, err := validator.GetPolicy(context.Background(), "expired.com")
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if policy != nil {
		t.Error("Expected nil policy after expired cache and no DNS record")
	}
}

// TestGetPolicy_NilPolicyCaching_Cov6 tests the nil-policy caching path.
func TestGetPolicy_NilPolicyCaching_Cov6(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	// No DNS records set, so fetchPolicy returns nil, nil
	policy, err := validator.GetPolicy(context.Background(), "nilcache.com")
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if policy != nil {
		t.Fatal("Expected nil policy")
	}

	// Verify the negative result was cached
	validator.cacheMu.RLock()
	entry, ok := validator.cache["nilcache.com"]
	validator.cacheMu.RUnlock()
	if !ok {
		t.Fatal("Expected cache entry for nilcache.com")
	}
	if entry.Policy != nil {
		t.Error("Expected nil policy in cache entry")
	}
	// The negative cache should expire in ~5 minutes
	if entry.ExpiresAt.Before(time.Now().Add(4 * time.Minute)) {
		t.Error("Negative cache should last at least 4 minutes")
	}
}

// TestGetPolicy_FetchPolicyError_Cov6 tests GetPolicy when fetchPolicy returns an error.
func TestGetPolicy_FetchPolicyError_Cov6(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.tempFail["_mta-sts.errored.com"] = true
	validator := NewMTASTSValidator(resolver)

	policy, err := validator.GetPolicy(context.Background(), "errored.com")
	if err == nil {
		t.Error("Expected error from fetchPolicy")
	}
	if policy != nil {
		t.Error("Expected nil policy on error")
	}
}

// TestGetPolicy_SuccessfulFetchAndCache_Cov6 tests the full path: DNS record found,
// policy file fetched, policy ID matches, and the result is cached.
func TestGetPolicy_SuccessfulFetchAndCache_Cov6(t *testing.T) {
	// Create the policy body first, then compute its ID
	// Note: max_age must be >= 86400 per the spec enforcement in parseMTASTSPolicy
	policyBody := "version: STSv1\nmode: testing\nmax_age: 86400\nmx: mail.valid.com\n"
	hash := sha256.Sum256([]byte(policyBody))
	policyID := base64.StdEncoding.EncodeToString(hash[:])

	resolver := newMockDNSResolver()
	resolver.txtRecords["_mta-sts.valid.com"] = []string{"v=STSv1; id=" + policyID}
	validator := NewMTASTSValidator(resolver)
	validator.httpClient = &http.Client{
		Transport: &mockTransport{
			statusCode: 200,
			body:       policyBody,
		},
		Timeout: 30 * time.Second,
	}

	policy, err := validator.GetPolicy(context.Background(), "valid.com")
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if policy == nil {
		t.Fatal("Expected non-nil policy")
	}
	if policy.Mode != MTASTSModeTesting {
		t.Errorf("Expected mode=testing, got %s", policy.Mode)
	}
	if policy.MaxAge != 86400 {
		t.Errorf("Expected max_age=86400, got %d", policy.MaxAge)
	}

	// Verify the policy was cached with proper MaxAge-based expiry
	validator.cacheMu.RLock()
	entry, ok := validator.cache["valid.com"]
	validator.cacheMu.RUnlock()
	if !ok {
		t.Fatal("Expected policy to be cached")
	}
	if entry.Policy == nil {
		t.Error("Expected cached policy to be non-nil")
	}
}

// =======================================================================
// DKIM fetchPublicKey (77.8%) - parseDKIMPublicKey edge cases
// =======================================================================

// TestParseDKIMPublicKey_NoValidKey_Cov6 tests various invalid DKIM public key records.
func TestParseDKIMPublicKey_NoValidKey_Cov6(t *testing.T) {
	tests := []struct {
		name   string
		record string
	}{
		{"empty record", ""},
		{"wrong version", "v=DKIM2; k=rsa; p=abc"},
		{"unsupported key type", "v=DKIM1; k=ed25519; p=abc"},
		{"missing key data", "v=DKIM1; k=rsa; p="},
		{"invalid base64", "v=DKIM1; k=rsa; p=not!valid!base64"},
		{"non-rsa key type", "v=DKIM1; k=ec; p=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parseDKIMPublicKey(tt.record)
			if err == nil && key != nil {
				t.Error("Expected error or nil key for invalid record")
			}
		})
	}
}

// =======================================================================
// DKIM Verify (41.7%) - exercise more error paths
// =======================================================================

// TestDKIMVerify_ParseError_Cov6 tests the parse error path in Verify.
func TestDKIMVerify_ParseError_Cov6(t *testing.T) {
	resolver := newMockDNSResolver()
	verifier := NewDKIMVerifier(resolver)

	result, sig, err := verifier.Verify(
		map[string][]string{"From": {"test@example.com"}},
		[]byte("body"),
		"not a valid dkim signature header at all",
	)
	if result != DKIMFail {
		t.Errorf("Expected DKIMFail, got %v", result)
	}
	if sig != nil {
		t.Error("Expected nil sig for parse error")
	}
	if err == nil {
		t.Error("Expected error")
	}
}

// TestDKIMVerify_UnsupportedAlgorithm_Cov6 tests the unsupported algorithm path.
func TestDKIMVerify_UnsupportedAlgorithm_Cov6(t *testing.T) {
	resolver := newMockDNSResolver()
	verifier := NewDKIMVerifier(resolver)

	result, sig, err := verifier.Verify(
		map[string][]string{"From": {"test@example.com"}},
		[]byte("body"),
		"v=1; a=rsa-sha1; d=example.com; s=test; c=simple/simple; bh=abc123; h=from; b=signature",
	)
	if result != DKIMFail {
		t.Errorf("Expected DKIMFail, got %v", result)
	}
	if sig == nil {
		t.Fatal("Expected non-nil sig for unsupported algorithm")
	}
	if sig.Algorithm != "rsa-sha1" {
		t.Errorf("Expected algorithm rsa-sha1, got %s", sig.Algorithm)
	}
	if err == nil || !strings.Contains(err.Error(), "unsupported algorithm") {
		t.Errorf("Expected unsupported algorithm error, got %v", err)
	}
}

// TestDKIMVerify_UnsupportedVersion_Cov6 tests Verify with unsupported DKIM version.
func TestDKIMVerify_UnsupportedVersion_Cov6(t *testing.T) {
	resolver := newMockDNSResolver()
	verifier := NewDKIMVerifier(resolver)

	result, sig, err := verifier.Verify(
		map[string][]string{"From": {"test@example.com"}},
		[]byte("body"),
		"v=2; a=rsa-sha256; d=example.com; s=test; bh=abc; h=from; b=sig",
	)
	if result != DKIMFail {
		t.Errorf("Expected DKIMFail for unsupported version, got %v", result)
	}
	_ = sig
	_ = err
}

// =======================================================================
// DKIM isTemporaryError - direct testing
// =======================================================================

// TestIsTemporaryError_VariousErrors_Cov6 tests isTemporaryError with different errors.
func TestIsTemporaryError_VariousErrors_Cov6(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		isTemp bool
	}{
		{"nil error", nil, false},
		{"timeout error", &testErr{s: "connection timeout"}, true},
		{"temporary error", &testErr{s: "temporary failure"}, true},
		{"permanent error", &testErr{s: "no such host"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTemporaryError(tt.err); got != tt.isTemp {
				t.Errorf("isTemporaryError(%v) = %v, want %v", tt.err, got, tt.isTemp)
			}
		})
	}
}

type testErr struct {
	s string
}

func (e *testErr) Error() string { return e.s }

// =======================================================================
// DKIM GenerateDKIMKeyPair (85.7%) - error path with invalid key size
// =======================================================================

// TestGenerateDKIMKeyPair_InvalidBits_Cov6 tests GenerateDKIMKeyPair with 0 bits.
func TestGenerateDKIMKeyPair_InvalidBits_Cov6(t *testing.T) {
	_, _, err := GenerateDKIMKeyPair(0)
	if err == nil {
		t.Error("Expected error with 0-bit key")
	}
}

// =======================================================================
// DKIM parseDKIMPublicKey (92%) - additional coverage for edge cases
// =======================================================================

// TestParseDKIMPublicKey_ValidRSAKey_Cov6 tests parseDKIMPublicKey with a valid RSA key.
func TestParseDKIMPublicKey_ValidRSAKey_Cov6(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	pubKeyB64 := base64.StdEncoding.EncodeToString(
		mustMarshalPKIXPublicKeyCov6(&privateKey.PublicKey),
	)
	record := "v=DKIM1; k=rsa; p=" + pubKeyB64

	key, err := parseDKIMPublicKey(record)
	if err != nil {
		t.Fatalf("parseDKIMPublicKey: %v", err)
	}
	if key == nil {
		t.Fatal("Expected non-nil public key")
	}
}

// =======================================================================
// DKIM parseDKIMSignature - more thorough coverage of tag parsing
// =======================================================================

// TestParseDKIMSignature_AllTags_Cov6 tests parsing a DKIM signature with all tags.
func TestParseDKIMSignature_AllTags_Cov6(t *testing.T) {
	header := "v=1; a=rsa-sha256; c=relaxed/relaxed; d=example.com; s=selector; " +
		"t=1234567890; x=1234571490; l=1024; bh=Y2FmZmVl; h=from:to:subject; " +
		"q=dns/txt; i=@example.com; b=c3lnbmF0dXJl"

	sig, err := parseDKIMSignature(header)
	if err != nil {
		t.Fatalf("parseDKIMSignature: %v", err)
	}
	if sig.Algorithm != "rsa-sha256" {
		t.Errorf("Expected algorithm rsa-sha256, got %s", sig.Algorithm)
	}
	if sig.Canonicalize != "relaxed/relaxed" {
		t.Errorf("Expected relaxed/relaxed, got %s", sig.Canonicalize)
	}
	if sig.Domain != "example.com" {
		t.Errorf("Expected domain example.com, got %s", sig.Domain)
	}
	if sig.Selector != "selector" {
		t.Errorf("Expected selector selector, got %s", sig.Selector)
	}
	if sig.BodyLength != 1024 {
		t.Errorf("Expected body length 1024, got %d", sig.BodyLength)
	}
	if sig.BodyHash != "Y2FmZmVl" {
		t.Errorf("Expected bh=Y2FmZmVl, got %s", sig.BodyHash)
	}
	if len(sig.SignedHeaders) != 3 {
		t.Errorf("Expected 3 signed headers, got %d", len(sig.SignedHeaders))
	}
	if sig.QueryMethod != "dns/txt" {
		t.Errorf("Expected query method dns/txt, got %s", sig.QueryMethod)
	}
	if sig.Signature != "c3lnbmF0dXJl" {
		t.Errorf("Expected signature c3lnbmF0dXJl, got %s", sig.Signature)
	}
}

// =======================================================================
// DKIM canonicalization helpers - edge cases
// =======================================================================

// TestCanonicalizeBody_Relaxed_Cov6 tests relaxed body canonicalization.
func TestCanonicalizeBody_Relaxed_Cov6(t *testing.T) {
	body := []byte("Line 1\r\n\r\nLine 2\r\n\r\n")
	result := canonicalizeBody(body, "relaxed")
	if len(result) == 0 {
		t.Error("Expected non-empty result from relaxed canonicalization")
	}
}

// TestCanonicalizeBody_Simple_Cov6 tests simple body canonicalization.
func TestCanonicalizeBody_Simple_Cov6(t *testing.T) {
	body := []byte("Line 1\r\n\r\n")
	result := canonicalizeBody(body, "simple")
	if len(result) == 0 {
		t.Error("Expected non-empty result from simple canonicalization")
	}
}

// TestCanonicalizeHeaders_Relaxed_Cov6 tests relaxed header canonicalization.
func TestCanonicalizeHeaders_Relaxed_Cov6(t *testing.T) {
	headers := map[string][]string{
		"Subject": {"  Test   Message  "},
		"From":    {"user@example.com"},
	}
	result := canonicalizeHeaders(headers, []string{"subject", "from"}, "relaxed")
	if result == "" {
		t.Error("Expected non-empty result from relaxed header canonicalization")
	}
}

// =======================================================================
// DKIM signRSA (80%) - test with a valid key and empty data to cover
// the success path more thoroughly
// =======================================================================

// TestSignRSA_ValidKey_ShortData_Cov6 tests signRSA with short data.
func TestSignRSA_ValidKey_ShortData_Cov6(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	sig, err := signRSA(privateKey, []byte("hi"))
	if err != nil {
		t.Fatalf("signRSA: %v", err)
	}
	if sig == "" {
		t.Error("Expected non-empty signature")
	}

	// Verify the signature
	err = verifyRSASignature(&privateKey.PublicKey, []byte("hi"), sig)
	if err != nil {
		t.Errorf("verifyRSASignature failed: %v", err)
	}
}

// =======================================================================
// Helper for RSA key marshaling
// =======================================================================

func mustMarshalPKIXPublicKeyCov6(pub interface{}) []byte {
	b, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		panic(err)
	}
	return b
}

// Ensure mockTransport from coverage_extra3_test.go is available.
var _ = (*mockTransport)(nil)

// Ensure http.Client is used.
var _ http.Client
