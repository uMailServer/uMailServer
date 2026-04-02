package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestGenerateCRAMMD5Challenge_Format_Cov2(t *testing.T) {
	challenge, b64, err := GenerateCRAMMD5Challenge()
	if err != nil {
		t.Fatalf("GenerateCRAMMD5Challenge: %v", err)
	}
	if !strings.HasPrefix(challenge, "<") || !strings.HasSuffix(challenge, "@umailserver>") {
		t.Errorf("unexpected challenge format: %s", challenge)
	}
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(decoded) != challenge {
		t.Errorf("b64 decoded != challenge: %q vs %q", string(decoded), challenge)
	}
}

func TestGenerateTOTPSecret_ValidBase32_Cov2(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	_, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Errorf("secret is not valid base32: %v", err)
	}
}

// =======================================================================
// user.go: VerifyCRAMMD5 - getSecret returning error
// =======================================================================

func TestVerifyCRAMMD5_GetSecretError_Cov2(t *testing.T) {
	challenge := base64.StdEncoding.EncodeToString([]byte("<test@umailserver>"))
	response := base64.StdEncoding.EncodeToString([]byte("user@example.com somedigest"))

	user, ok := VerifyCRAMMD5(challenge, response, func(u string) (string, error) {
		return "", fmt.Errorf("database error")
	})
	if ok {
		t.Error("expected false when getSecret returns error")
	}
	if user != "user@example.com" {
		t.Errorf("expected username returned, got %s", user)
	}
}

// =======================================================================
// arc.go: Validate (78.1%) - multiple ARC sets
// =======================================================================

func TestARCValidator_Validate_MultipleSets_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewARCValidator(resolver)
	headers := map[string][]string{
		"ARC-Authentication-Results": {"i=1; auth=pass", "i=2; auth=pass"},
		"ARC-Message-Signature": {
			"i=1; a=rsa-sha256; d=example.com; s=selector; b=sig1",
			"i=2; a=rsa-sha256; d=example.com; s=selector; b=sig2",
		},
		"ARC-Seal": {
			"i=1; a=rsa-sha256; d=example.com; s=selector; cv=none; b=seal1",
			"i=2; a=rsa-sha256; d=example.com; s=selector; cv=pass; b=seal2",
		},
	}

	chain, err := validator.Validate(context.Background(), headers, []byte("body"))
	if err != nil {
		t.Logf("Validate: %v", err)
	}
	if chain == nil {
		t.Error("expected non-nil chain")
	}
}

func TestARCValidator_Validate_EmptyHeaders_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewARCValidator(resolver)
	headers := map[string][]string{}

	chain, err := validator.Validate(context.Background(), headers, nil)
	if err != nil {
		t.Fatalf("Validate with empty headers: %v", err)
	}
	if chain.CV != "none" {
		t.Errorf("expected CV=none, got %s", chain.CV)
	}
}

func TestARCSigner_NilPrivateKey_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	signer := NewARCSigner(resolver, nil, "example.com", "selector")
	_, err := signer.Sign(nil, nil, "", 1)
	if err == nil {
		t.Error("expected error with nil private key")
	}
}

func TestARCSigner_SignWithKey_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer := NewARCSigner(resolver, key, "example.com", "selector")

	headers := map[string][]string{
		"From":    {"sender@example.com"},
		"To":      {"recipient@example.com"},
		"Subject": {"Test"},
	}

	set, err := signer.Sign(headers, []byte("test body"), "auth=pass", 1)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if set == nil {
		t.Fatal("expected non-nil ARC set")
	}
	if set.Instance != 1 {
		t.Errorf("expected instance 1, got %d", set.Instance)
	}
	if set.AAR == "" {
		t.Error("expected non-empty AAR")
	}
	if set.AMS == "" {
		t.Error("expected non-empty AMS")
	}
	if set.AS == "" {
		t.Error("expected non-empty AS")
	}
}

func TestARCSigner_SignDeterminesCV_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer := NewARCSigner(resolver, key, "example.com", "sel")

	// With no existing ARC headers, CV should be "none"
	headers := map[string][]string{
		"From": {"test@example.com"},
	}
	set, err := signer.Sign(headers, []byte("body"), "auth=pass", 1)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.Contains(set.AS, "cv=none") {
		t.Errorf("expected cv=none in AS, got: %s", set.AS)
	}
}

func TestARCSigner_SignWithExistingARC_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer := NewARCSigner(resolver, key, "example.com", "sel")

	headers := map[string][]string{
		"From":     {"test@example.com"},
		"ARC-Seal": {"i=1; a=rsa-sha256; d=example.com; s=sel; cv=pass; b=sig"},
	}
	set, err := signer.Sign(headers, []byte("body"), "auth=pass", 2)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.Contains(set.AS, "cv=pass") {
		t.Errorf("expected cv=pass in AS for instance 2, got: %s", set.AS)
	}
}

// =======================================================================
// arc.go: groupARCHeaders (92.3%) - sorting with different instances
// =======================================================================

func TestGroupARCHeaders_MultipleSets_Cov2(t *testing.T) {
	entries := []headerEntry{
		{Name: "arc-authentication-results", Value: "i=2; auth=pass"},
		{Name: "arc-message-signature", Value: "i=2; a=rsa-sha256; b=sig2"},
		{Name: "arc-seal", Value: "i=2; a=rsa-sha256; cv=pass; b=seal2"},
		{Name: "arc-authentication-results", Value: "i=1; auth=pass"},
		{Name: "arc-message-signature", Value: "i=1; a=rsa-sha256; b=sig1"},
		{Name: "arc-seal", Value: "i=1; a=rsa-sha256; cv=none; b=seal1"},
	}
	groups := groupARCHeaders(entries)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	// Map iteration order is non-deterministic; just verify both instances exist
	instances := map[int]bool{}
	for _, g := range groups {
		instances[g.Instance] = true
	}
	if !instances[1] || !instances[2] {
		t.Errorf("expected instances 1 and 2, got %v", instances)
	}
}

// =======================================================================
// dane.go: Validate (85.7%) - TLSA records, peer cert validation
// =======================================================================

func TestDANEValidator_NoTLSARecords_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{}},
	}
	result, err := validator.Validate("example.com", 25, state)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result != DANENone {
		t.Errorf("expected DANENone, got %v", result)
	}
}

func TestDANEValidator_NoPeerCertificates_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["_25._tcp.example.com"] = []string{"3 1 1 abc123def456"}
	validator := NewDANEValidator(resolver)
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{},
	}
	result, err := validator.Validate("example.com", 25, state)
	if err == nil {
		t.Error("expected error for no peer certs")
	}
	if result != DANEFailed {
		t.Errorf("expected DANEFailed, got %v", result)
	}
}

func TestDANEValidator_WithPeerCerts_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["_25._tcp.example.com"] = []string{"3 1 1 abc123def456"}
	validator := NewDANEValidator(resolver)
	cert := &x509.Certificate{
		Raw: []byte("fake-cert-data"),
	}
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}
	result, err := validator.Validate("example.com", 25, state)
	t.Logf("Validate result: %v, err: %v", result, err)
}

func TestParseTLSARecord_ValidFormat_Cov2(t *testing.T) {
	record, err := parseTLSARecord("3 1 1 abc123")
	if err != nil {
		t.Fatalf("parseTLSARecord: %v", err)
	}
	if record.Usage != TLSAUsage(3) {
		t.Errorf("expected usage 3, got %d", record.Usage)
	}
	if record.Selector != TLSASelector(1) {
		t.Errorf("expected selector 1, got %d", record.Selector)
	}
	if record.MatchingType != TLSAMatchingType(1) {
		t.Errorf("expected matching type 1, got %d", record.MatchingType)
	}
}

func TestParseTLSARecord_InvalidUsage_Cov2(t *testing.T) {
	_, err := parseTLSARecord("x 1 1 abc123")
	if err == nil {
		t.Error("expected error for invalid usage")
	}
}

func TestParseTLSARecord_InvalidSelector_Cov2(t *testing.T) {
	_, err := parseTLSARecord("3 x 1 abc123")
	if err == nil {
		t.Error("expected error for invalid selector")
	}
}

func TestParseTLSARecord_InvalidMatchingType_Cov2(t *testing.T) {
	_, err := parseTLSARecord("3 1 x abc123")
	if err == nil {
		t.Error("expected error for invalid matching type")
	}
}

func TestParseTLSARecord_HexFallback_Cov2(t *testing.T) {
	// Less than 4 space-separated parts triggers hex fallback
	record, err := parseTLSARecord("abcdef123456")
	if err != nil {
		t.Logf("parseTLSARecord hex fallback: %v", err)
	}
	_ = record
}

func TestParseTLSAHex_TooShort_Cov2(t *testing.T) {
	_, err := parseTLSAHex("abcd")
	if err == nil {
		t.Error("expected error for hex too short")
	}
}

func TestParseTLSAHex_InvalidHex_Cov2(t *testing.T) {
	_, err := parseTLSAHex("xyz1234567890")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

// =======================================================================
// dane.go: validateRecord - DANE-EE, full cert, SPKI, SHA256
// =======================================================================

func TestDANEValidator_ValidateRecord_DANEEE_FullCert_Cov2(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(certDER)

	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	// DANE-EE (usage=3), Full Cert (selector=0), Exact match (matching=0)
	tlsa := &TLSARecord{
		Usage:        TLSAUsage(3),
		Selector:     TLSASelector(0),
		MatchingType: TLSAMatchingType(0),
		Certificate:  certDER,
	}
	result := validator.validateRecord(tlsa, cert, nil)
	if !result {
		t.Error("expected validation to succeed with exact cert match")
	}
}

func TestDANEValidator_ValidateRecord_SPKI_SHA256_Cov2(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(certDER)

	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	// SPKI selector with SHA256
	spkiHash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	tlsa := &TLSARecord{
		Usage:        TLSAUsage(3),
		Selector:     TLSASelector(1),
		MatchingType: TLSAMatchingType(1),
		Certificate:  spkiHash[:],
	}
	result := validator.validateRecord(tlsa, cert, nil)
	if !result {
		t.Error("expected SPKI SHA256 match")
	}
}

func TestDANEValidator_ValidateRecord_NoMatch_Cov2(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(certDER)

	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	tlsa := &TLSARecord{
		Usage:        TLSAUsage(3),
		Selector:     TLSASelector(0),
		MatchingType: TLSAMatchingType(1),
		Certificate:  []byte("wronghash"),
	}
	result := validator.validateRecord(tlsa, cert, nil)
	if result {
		t.Error("expected no match with wrong hash")
	}
}

func TestDANEValidator_ValidateRecord_UnsupportedSelector_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	cert := &x509.Certificate{Raw: []byte("data")}
	tlsa := &TLSARecord{
		Usage:        TLSAUsage(3),
		Selector:     TLSASelector(99),
		MatchingType: TLSAMatchingType(1),
		Certificate:  []byte("hash"),
	}
	result := validator.validateRecord(tlsa, cert, nil)
	if result {
		t.Error("expected false for unsupported selector")
	}
}

// =======================================================================
// dane.go: DANE methods
// =======================================================================

func TestDANEValidator_ValidateMX_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{}},
	}
	result, err := validator.ValidateMX("mail.example.com", state)
	t.Logf("ValidateMX result: %v, err: %v", result, err)
}

func TestDANEValidator_ValidateSubmission_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{}},
	}
	result, err := validator.ValidateSubmission("example.com", state)
	t.Logf("ValidateSubmission result: %v, err: %v", result, err)
}

func TestDANEValidator_IsDANEAvailable_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	available, err := validator.IsDANEAvailable("example.com", 25)
	t.Logf("IsDANEAvailable: %v, err: %v", available, err)
}

func TestDANEValidator_GetPolicy_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	policy, err := validator.GetPolicy("example.com", 25)
	t.Logf("GetPolicy: %v, err: %v", policy, err)
}

func TestDANEValidator_ValidateWithDNSSEC_Cov2(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{}},
	}
	result, err := validator.ValidateWithDNSSEC("example.com", 25, state, DNSSECSecured)
	t.Logf("ValidateWithDNSSEC result: %v, err: %v", result, err)
}

func TestDANEValidator_ValidateWithDNSSEC_NotSecured(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{}},
	}
	result, err := validator.ValidateWithDNSSEC("example.com", 25, state, DNSSECInsecure)
	if err != nil {
		t.Fatalf("ValidateWithDNSSEC: %v", err)
	}
	if result != DANENone {
		t.Errorf("expected DANENone for non-secured DNSSEC, got %v", result)
	}
}

// =======================================================================
// dane.go: DANEResult.String()
// =======================================================================

func TestDANEResult_String_Cov2(t *testing.T) {
	tests := []struct {
		result   DANEResult
		expected string
	}{
		{DANENone, "none"},
		{DANEValidated, "validated"},
		{DANEFailed, "failed"},
		{DANEUnusable, "unusable"},
		{DANEResult(99), "unknown"},
	}
	for _, tt := range tests {
		s := tt.result.String()
		if s != tt.expected {
			t.Errorf("DANEResult(%d).String() = %q, want %q", tt.result, s, tt.expected)
		}
	}
}

// =======================================================================
// arc.go: determineCV - ARC-Seal with mixed case headers
// =======================================================================

func TestDetermineCV_MixedCaseHeaders_Cov2(t *testing.T) {
	headers := map[string][]string{
		"ARC-Seal": {"i=1; a=rsa-sha256; d=example.com; s=sel; cv=pass; b=sig"},
	}
	cv := determineCV(headers)
	if cv != "pass" {
		t.Errorf("expected cv=pass, got %s", cv)
	}
}

func TestDetermineCV_LowercaseHeaders_Cov2(t *testing.T) {
	headers := map[string][]string{
		"arc-seal": {"i=1; a=rsa-sha256; d=example.com; s=sel; cv=none; b=sig"},
	}
	cv := determineCV(headers)
	if cv != "none" {
		t.Errorf("expected cv=none, got %s", cv)
	}
}

func TestDetermineCV_NoARCSeals_Cov2(t *testing.T) {
	headers := map[string][]string{
		"From": {"test@example.com"},
	}
	cv := determineCV(headers)
	if cv != "none" {
		t.Errorf("expected cv=none when no seals, got %s", cv)
	}
}

// =======================================================================
// dane.go: lookupTLSARecords - isTemporaryError path
// =======================================================================

func TestDANEValidator_LookupTLSARecords_Error(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.failLookup["_25._tcp.example.com"] = true
	validator := NewDANEValidator(resolver)
	records, err := validator.LookupTLSA("example.com", 25)
	t.Logf("LookupTLSA error case: records=%v, err=%v", records, err)
}

// =======================================================================
// dane.go: validateRecord - full cert exact match and SHA512 unsupported
// =======================================================================

func TestDANEValidator_ValidateRecord_SHA512_Unsupported(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)
	cert := &x509.Certificate{Raw: []byte("certdata")}
	tlsa := &TLSARecord{
		Usage:        TLSAUsage(3),
		Selector:     TLSASelector(0),
		MatchingType: TLSAMatchingType(2), // SHA-512
		Certificate:  []byte("hash"),
	}
	result := validator.validateRecord(tlsa, cert, nil)
	if result {
		t.Error("expected false for unsupported SHA-512 matching")
	}
}
