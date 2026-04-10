package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"testing"
	"time"
)

func TestDANEResultString(t *testing.T) {
	tests := []struct {
		result   DANEResult
		expected string
	}{
		{DANENone, "none"},
		{DANEValidated, "validated"},
		{DANEFailed, "failed"},
		{DANEUnusable, "unusable"},
		{DANEResult(999), "unknown"},
	}

	for _, tt := range tests {
		got := tt.result.String()
		if got != tt.expected {
			t.Errorf("DANEResult(%d).String() = %q, want %q", tt.result, got, tt.expected)
		}
	}
}

func TestNewDANEValidator(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	if validator == nil {
		t.Fatal("NewDANEValidator returned nil")
	}
	if validator.resolver != resolver {
		t.Error("Resolver not set correctly")
	}
}

func TestParseTLSARecord(t *testing.T) {
	// Test parsing TLSA record: "3 1 1 abc123..."
	// Usage 3 (DANE-EE), Selector 1 (SPKI), MatchingType 1 (SHA-256)
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i)
	}

	tests := []struct {
		name         string
		data         string
		wantUsage    TLSAUsage
		wantSelector TLSASelector
		wantMatch    TLSAMatchingType
		wantErr      bool
	}{
		{
			name:         "valid record - text format",
			data:         "3 1 1 " + hex.EncodeToString(hash),
			wantUsage:    TLSAUsageDANEEE,
			wantSelector: TLSASelectorSPKI,
			wantMatch:    TLSAMatchingTypeSHA256,
			wantErr:      false,
		},
		{
			name:    "too short",
			data:    "3 1",
			wantErr: true,
		},
		{
			name:    "invalid usage",
			data:    "x 1 1 abc",
			wantErr: true,
		},
		{
			name:    "invalid hex",
			data:    "3 1 1 xyz",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, err := parseTLSARecord(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if record.Usage != tt.wantUsage {
				t.Errorf("Usage = %d, want %d", record.Usage, tt.wantUsage)
			}
			if record.Selector != tt.wantSelector {
				t.Errorf("Selector = %d, want %d", record.Selector, tt.wantSelector)
			}
			if record.MatchingType != tt.wantMatch {
				t.Errorf("MatchingType = %d, want %d", record.MatchingType, tt.wantMatch)
			}
		})
	}
}

func TestParseTLSAHex(t *testing.T) {
	// Create a TLSA record in hex format
	// Usage: 3, Selector: 1, MatchingType: 1, Data: 32 bytes SHA-256 hash
	data := make([]byte, 35) // 3 header bytes + 32 hash bytes
	data[0] = 3              // DANE-EE
	data[1] = 1              // SPKI
	data[2] = 1              // SHA-256
	for i := 3; i < 35; i++ {
		data[i] = byte(i)
	}

	hexStr := hex.EncodeToString(data)

	record, err := parseTLSAHex(hexStr)
	if err != nil {
		t.Fatalf("parseTLSAHex failed: %v", err)
	}

	if record.Usage != TLSAUsageDANEEE {
		t.Errorf("Usage = %d, want %d", record.Usage, TLSAUsageDANEEE)
	}

	if record.Selector != TLSASelectorSPKI {
		t.Errorf("Selector = %d, want %d", record.Selector, TLSASelectorSPKI)
	}

	if record.MatchingType != TLSAMatchingTypeSHA256 {
		t.Errorf("MatchingType = %d, want %d", record.MatchingType, TLSAMatchingTypeSHA256)
	}
}

func TestTLSARecordString(t *testing.T) {
	record := &TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorSPKI,
		MatchingType: TLSAMatchingTypeSHA256,
		Certificate:  []byte{0x01, 0x02, 0x03, 0x04},
	}

	str := record.String()

	// Should contain: "3 1 1 01020304"
	if str != "3 1 1 01020304" {
		t.Errorf("String() = %q, expected 3 1 1 01020304", str)
	}
}

func TestGenerateTLSARecord(t *testing.T) {
	// Generate a test certificate
	cert := generateTestCert(t)

	record := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeSHA256)

	if record == nil {
		t.Fatal("GenerateTLSARecord returned nil")
	}

	if record.Usage != TLSAUsageDANEEE {
		t.Errorf("Usage = %d, want %d", record.Usage, TLSAUsageDANEEE)
	}

	if record.Selector != TLSASelectorSPKI {
		t.Errorf("Selector = %d, want %d", record.Selector, TLSASelectorSPKI)
	}

	if record.MatchingType != TLSAMatchingTypeSHA256 {
		t.Errorf("MatchingType = %d, want %d", record.MatchingType, TLSAMatchingTypeSHA256)
	}

	// Certificate data should be SHA-256 hash (32 bytes)
	if len(record.Certificate) != 32 {
		t.Errorf("Certificate length = %d, want 32", len(record.Certificate))
	}
}

func TestDANEPolicy(t *testing.T) {
	// Test DANE policy struct
	policy := &DANEPolicy{
		Domain:       "example.com",
		Port:         25,
		HasTLSA:      true,
		Usages:       []TLSAUsage{TLSAUsageDANEEE},
		ValidRecords: 1,
	}

	if policy.Domain != "example.com" {
		t.Error("Domain mismatch")
	}

	if policy.Port != 25 {
		t.Error("Port mismatch")
	}

	if !policy.HasTLSA {
		t.Error("HasTLSA should be true")
	}
}

func TestDNSSECStatus(t *testing.T) {
	// Test that DNSSEC status constants exist
	statuses := []DNSSECStatus{
		DNSSECUnknown,
		DNSSECSecured,
		DNSSECInsecure,
		DNSSECBogus,
	}

	if len(statuses) != 4 {
		t.Error("Expected 4 DNSSEC status values")
	}
}

// Helper function to generate a test certificate
func generateTestCert(t *testing.T) *x509.Certificate {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"example.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	return cert
}

func TestDANEValidateNoRecords(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	// Create a mock TLS connection state
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{},
	}

	result, err := validator.Validate("example.com", 25, state)

	// Should return DANENone since no TLSA records
	if result != DANENone {
		t.Errorf("Expected DANENone, got %s", result.String())
	}

	// No error expected
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestDANEValidateNoCertificates(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	// Create TLSA record in DNS
	hash := make([]byte, 32)
	record := &TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorSPKI,
		MatchingType: TLSAMatchingTypeSHA256,
		Certificate:  hash,
	}
	resolver.txtRecords["_25._tcp.example.com"] = []string{record.String()}

	// Create a mock TLS connection state with no certificates
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{},
	}

	result, err := validator.Validate("example.com", 25, state)

	// Should return DANEFailed since no certificates
	if result != DANEFailed {
		t.Errorf("Expected DANEFailed, got %s", result.String())
	}

	// Should return error
	if err == nil {
		t.Error("Expected error for no certificates")
	}
}

func TestDANEIsDANEAvailable(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	// Initially no TLSA records
	available, err := validator.IsDANEAvailable("example.com", 25)
	if err != nil {
		t.Fatalf("IsDANEAvailable failed: %v", err)
	}
	if available {
		t.Error("Expected DANE not available")
	}
}

func TestTLSAUsageConstants(t *testing.T) {
	// Test that TLSA usage constants have correct values
	if TLSAUsagePKITAAncillary != 0 {
		t.Error("TLSAUsagePKITAAncillary should be 0")
	}
	if TLSAUsagePKITEEAncillary != 1 {
		t.Error("TLSAUsagePKITEEAncillary should be 1")
	}
	if TLSAUsageDANETA != 2 {
		t.Error("TLSAUsageDANETA should be 2")
	}
	if TLSAUsageDANEEE != 3 {
		t.Error("TLSAUsageDANEEE should be 3")
	}
}

func TestTLSASelectorConstants(t *testing.T) {
	if TLSASelectorFullCert != 0 {
		t.Error("TLSASelectorFullCert should be 0")
	}
	if TLSASelectorSPKI != 1 {
		t.Error("TLSASelectorSPKI should be 1")
	}
}

func TestTLSAMatchingTypeConstants(t *testing.T) {
	if TLSAMatchingTypeFull != 0 {
		t.Error("TLSAMatchingTypeFull should be 0")
	}
	if TLSAMatchingTypeSHA256 != 1 {
		t.Error("TLSAMatchingTypeSHA256 should be 1")
	}
	if TLSAMatchingTypeSHA512 != 2 {
		t.Error("TLSAMatchingTypeSHA512 should be 2")
	}
}

func TestDANEValidateWithMatchingCert(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	// Generate a test certificate
	cert := generateTestCert(t)

	// Generate TLSA record from certificate
	record := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeSHA256)

	// Add TLSA record to resolver
	resolver.txtRecords["_25._tcp.example.com"] = []string{record.String()}

	// Create TLS connection state with certificate
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}

	result, err := validator.Validate("example.com", 25, state)

	// Should return DANEValidated
	if result != DANEValidated {
		t.Errorf("Expected DANEValidated, got %s", result.String())
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestDANEValidateWithNonMatchingCert(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	// Generate two different certificates
	cert1 := generateTestCert(t)
	cert2 := generateTestCert(t)

	// Generate TLSA record from cert1
	record := GenerateTLSARecord(cert1, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeSHA256)

	// Add TLSA record to resolver
	resolver.txtRecords["_25._tcp.example.com"] = []string{record.String()}

	// Create TLS connection state with different certificate (cert2)
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert2},
	}

	result, _ := validator.Validate("example.com", 25, state)

	// Should return DANEFailed due to hash mismatch
	if result != DANEFailed {
		t.Errorf("Expected DANEFailed, got %s", result.String())
	}
	// Note: Error may or may not be returned depending on implementation
}

// Test ValidateMX function
func TestValidateMX(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	// Generate a test certificate
	cert := generateTestCert(t)

	// Generate TLSA record from certificate for port 25
	record := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeSHA256)
	resolver.txtRecords["_25._tcp.mail.example.com"] = []string{record.String()}

	// Create TLS connection state with certificate
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}

	// ValidateMX should work with port 25
	result, err := validator.ValidateMX("mail.example.com", state)

	if result != DANEValidated {
		t.Errorf("Expected DANEValidated, got %s", result.String())
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// Test ValidateSubmission function
func TestValidateSubmission(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	// Generate a test certificate
	cert := generateTestCert(t)

	// Generate TLSA record from certificate for port 587
	record := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeSHA256)
	resolver.txtRecords["_587._tcp.example.com"] = []string{record.String()}

	// Create TLS connection state with certificate
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}

	// ValidateSubmission should work with port 587
	result, err := validator.ValidateSubmission("example.com", state)

	if result != DANEValidated {
		t.Errorf("Expected DANEValidated, got %s", result.String())
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// Test GetPolicy function
func TestGetPolicy(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	// Create TLSA records
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i)
	}

	// Add DANE-TA usage record (should count as valid)
	record1 := &TLSARecord{
		Usage:        TLSAUsageDANETA,
		Selector:     TLSASelectorSPKI,
		MatchingType: TLSAMatchingTypeSHA256,
		Certificate:  hash,
	}

	// Add DANE-EE usage record (should count as valid)
	record2 := &TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorSPKI,
		MatchingType: TLSAMatchingTypeSHA256,
		Certificate:  hash,
	}

	resolver.txtRecords["_25._tcp.example.com"] = []string{
		record1.String(),
		record2.String(),
	}

	policy, err := validator.GetPolicy("example.com", 25)
	if err != nil {
		t.Fatalf("GetPolicy failed: %v", err)
	}

	if policy.Domain != "example.com" {
		t.Errorf("Expected domain 'example.com', got %q", policy.Domain)
	}

	if policy.Port != 25 {
		t.Errorf("Expected port 25, got %d", policy.Port)
	}

	if !policy.HasTLSA {
		t.Error("Expected HasTLSA to be true")
	}

	// Should have 2 valid records (DANE-TA and DANE-EE)
	if policy.ValidRecords != 2 {
		t.Errorf("Expected 2 valid records, got %d", policy.ValidRecords)
	}

	// Should have 2 unique usages
	if len(policy.Usages) != 2 {
		t.Errorf("Expected 2 usages, got %d", len(policy.Usages))
	}
}

// Test GetPolicy with no records
func TestGetPolicyNoRecords(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	policy, err := validator.GetPolicy("example.com", 25)
	if err != nil {
		t.Fatalf("GetPolicy failed: %v", err)
	}

	if policy.Domain != "example.com" {
		t.Errorf("Expected domain 'example.com', got %q", policy.Domain)
	}

	if policy.HasTLSA {
		t.Error("Expected HasTLSA to be false when no records")
	}

	if policy.ValidRecords != 0 {
		t.Errorf("Expected 0 valid records, got %d", policy.ValidRecords)
	}
}

// Test ValidateWithDNSSEC function
func TestValidateWithDNSSEC(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	// Generate a test certificate
	cert := generateTestCert(t)

	// Generate TLSA record from certificate
	record := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeSHA256)
	resolver.txtRecords["_25._tcp.example.com"] = []string{record.String()}

	// Create TLS connection state with certificate
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}

	// Test with DNSSEC secured - should validate
	result, err := validator.ValidateWithDNSSEC("example.com", 25, state, DNSSECSecured)
	if result != DANEValidated {
		t.Errorf("Expected DANEValidated with DNSSEC, got %s", result.String())
	}
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Test with DNSSEC unknown - should return DANENone
	result, err = validator.ValidateWithDNSSEC("example.com", 25, state, DNSSECUnknown)
	if result != DANENone {
		t.Errorf("Expected DANENone without DNSSEC, got %s", result.String())
	}
	if err != nil {
		t.Errorf("Unexpected error with DNSSEC unknown: %v", err)
	}

	// Test with DNSSEC insecure - should return DANENone
	result, err = validator.ValidateWithDNSSEC("example.com", 25, state, DNSSECInsecure)
	if result != DANENone {
		t.Errorf("Expected DANENone with insecure DNSSEC, got %s", result.String())
	}
	if err != nil {
		t.Errorf("Unexpected error with DNSSEC insecure: %v", err)
	}

	// Test with DNSSEC bogus - should return DANENone
	result, err = validator.ValidateWithDNSSEC("example.com", 25, state, DNSSECBogus)
	if result != DANENone {
		t.Errorf("Expected DANENone with bogus DNSSEC, got %s", result.String())
	}
	if err != nil {
		t.Errorf("Unexpected error with DNSSEC bogus: %v", err)
	}
}

// Test GetPolicy with non-DANE usages (should not count as valid)
func TestGetPolicyWithNonDANEUsages(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	hash := make([]byte, 32)

	// Add PKI-TA usage record (should NOT count as valid DANE)
	record1 := &TLSARecord{
		Usage:        TLSAUsagePKITAAncillary,
		Selector:     TLSASelectorSPKI,
		MatchingType: TLSAMatchingTypeSHA256,
		Certificate:  hash,
	}

	// Add PKI-EE usage record (should NOT count as valid DANE)
	record2 := &TLSARecord{
		Usage:        TLSAUsagePKITEEAncillary,
		Selector:     TLSASelectorSPKI,
		MatchingType: TLSAMatchingTypeSHA256,
		Certificate:  hash,
	}

	resolver.txtRecords["_25._tcp.example.com"] = []string{
		record1.String(),
		record2.String(),
	}

	policy, err := validator.GetPolicy("example.com", 25)
	if err != nil {
		t.Fatalf("GetPolicy failed: %v", err)
	}

	// Has records but none are valid DANE records
	if !policy.HasTLSA {
		t.Error("Expected HasTLSA to be true (has records)")
	}

	// ValidRecords should be 0 since neither PKI-TA nor PKI-EE count
	if policy.ValidRecords != 0 {
		t.Errorf("Expected 0 valid DANE records, got %d", policy.ValidRecords)
	}
}

// --- Additional coverage tests ---

func TestGenerateTLSARecordFullCert(t *testing.T) {
	cert := generateTestCert(t)
	record := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorFullCert, TLSAMatchingTypeSHA256)
	if record == nil {
		t.Fatal("Expected non-nil record")
	}
	if len(record.Certificate) != 32 {
		t.Errorf("Expected 32 bytes for SHA-256, got %d", len(record.Certificate))
	}
}

func TestGenerateTLSARecordFullMatching(t *testing.T) {
	cert := generateTestCert(t)
	record := GenerateTLSARecord(cert, TLSAUsageDANETA, TLSASelectorFullCert, TLSAMatchingTypeFull)
	if record == nil {
		t.Fatal("Expected non-nil record")
	}
	// Full matching: Certificate should be the raw cert bytes
	if !equalBytes(record.Certificate, cert.Raw) {
		t.Error("Expected Certificate to match cert.Raw for full matching type")
	}
}

func TestGenerateTLSARecordSPKIFull(t *testing.T) {
	cert := generateTestCert(t)
	record := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeFull)
	if record == nil {
		t.Fatal("Expected non-nil record")
	}
	if !equalBytes(record.Certificate, cert.RawSubjectPublicKeyInfo) {
		t.Error("Expected Certificate to match RawSubjectPublicKeyInfo for SPKI+full")
	}
}

func TestGenerateTLSARecordSHA512(t *testing.T) {
	cert := generateTestCert(t)
	record := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeSHA512)
	if record == nil {
		t.Fatal("Expected non-nil record for SHA-512")
	}
	// Certificate should be SHA-512 hash of SPKI
	expected := computeSHA512(cert.RawSubjectPublicKeyInfo)
	if !equalBytes(record.Certificate, expected) {
		t.Error("Expected Certificate to match SHA-512 hash of SPKI")
	}
}

func TestValidateRecordFullCertSHA256(t *testing.T) {
	cert := generateTestCert(t)
	validator := NewDANEValidator(newMockDNSResolver())

	tlsa := &TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorFullCert,
		MatchingType: TLSAMatchingTypeSHA256,
		Certificate:  computeSHA256(cert.Raw),
	}

	result := validator.validateRecord(tlsa, cert, &tls.ConnectionState{})
	if !result {
		t.Error("Expected validateRecord to return true for matching full cert SHA-256")
	}
}

func TestValidateRecordSPKISHA256(t *testing.T) {
	cert := generateTestCert(t)
	validator := NewDANEValidator(newMockDNSResolver())

	tlsa := &TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorSPKI,
		MatchingType: TLSAMatchingTypeSHA256,
		Certificate:  computeSHA256(cert.RawSubjectPublicKeyInfo),
	}

	result := validator.validateRecord(tlsa, cert, &tls.ConnectionState{})
	if !result {
		t.Error("Expected validateRecord to return true for matching SPKI SHA-256")
	}
}

func TestValidateRecordFullMatching(t *testing.T) {
	cert := generateTestCert(t)
	validator := NewDANEValidator(newMockDNSResolver())

	tlsa := &TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorFullCert,
		MatchingType: TLSAMatchingTypeFull,
		Certificate:  cert.Raw,
	}

	result := validator.validateRecord(tlsa, cert, &tls.ConnectionState{})
	if !result {
		t.Error("Expected validateRecord to return true for full matching")
	}
}

func TestValidateRecordSPKIFullMatching(t *testing.T) {
	cert := generateTestCert(t)
	validator := NewDANEValidator(newMockDNSResolver())

	tlsa := &TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorSPKI,
		MatchingType: TLSAMatchingTypeFull,
		Certificate:  cert.RawSubjectPublicKeyInfo,
	}

	result := validator.validateRecord(tlsa, cert, &tls.ConnectionState{})
	if !result {
		t.Error("Expected validateRecord to return true for SPKI full matching")
	}
}

func TestValidateRecordUnsupportedSelector(t *testing.T) {
	cert := generateTestCert(t)
	validator := NewDANEValidator(newMockDNSResolver())

	tlsa := &TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelector(99),
		MatchingType: TLSAMatchingTypeSHA256,
		Certificate:  []byte{},
	}

	result := validator.validateRecord(tlsa, cert, &tls.ConnectionState{})
	if result {
		t.Error("Expected false for unsupported selector")
	}
}

func TestValidateRecordSHA512(t *testing.T) {
	cert := generateTestCert(t)
	validator := NewDANEValidator(newMockDNSResolver())

	tlsa := &TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorFullCert,
		MatchingType: TLSAMatchingTypeSHA512,
		Certificate:  computeSHA512(cert.Raw),
	}

	result := validator.validateRecord(tlsa, cert, &tls.ConnectionState{})
	if !result {
		t.Error("Expected true for matching SHA-512 certificate")
	}
}

func TestValidateRecordUnsupportedMatching(t *testing.T) {
	cert := generateTestCert(t)
	validator := NewDANEValidator(newMockDNSResolver())

	tlsa := &TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorFullCert,
		MatchingType: TLSAMatchingType(99),
		Certificate:  []byte{},
	}

	result := validator.validateRecord(tlsa, cert, &tls.ConnectionState{})
	if result {
		t.Error("Expected false for unsupported matching type")
	}
}

func TestDANEValidateWithCertMatchDANETA(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewDANEValidator(resolver)

	cert := generateTestCert(t)
	record := GenerateTLSARecord(cert, TLSAUsageDANETA, TLSASelectorSPKI, TLSAMatchingTypeSHA256)
	resolver.txtRecords["_25._tcp.example.com"] = []string{record.String()}

	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}

	result, err := validator.Validate("example.com", 25, state)
	if result != DANEValidated {
		t.Errorf("Expected DANEValidated for DANE-TA, got %s: %v", result.String(), err)
	}
}

func TestParseTLSARecordInvalidSelector(t *testing.T) {
	_, err := parseTLSARecord("3 x 1 abc")
	if err == nil {
		t.Error("Expected error for invalid selector")
	}
}

func TestParseTLSARecordInvalidMatchingType(t *testing.T) {
	_, err := parseTLSARecord("3 1 x abc")
	if err == nil {
		t.Error("Expected error for invalid matching type")
	}
}

func TestParseTLSARecordOddHex(t *testing.T) {
	_, err := parseTLSARecord("3 1 1 xyz")
	if err == nil {
		t.Error("Expected error for odd-length hex")
	}
}

func TestParseTLSAHexOddLength(t *testing.T) {
	_, err := parseTLSAHex("abc")
	if err == nil {
		t.Error("Expected error for odd-length hex")
	}
}

func TestParseTLSAHexTooShort(t *testing.T) {
	_, err := parseTLSAHex("aabb")
	if err == nil {
		t.Error("Expected error for too short data (< 3 bytes)")
	}
}

func computeSHA256(data []byte) []byte {
	hash := make([]byte, 32)
	h := sha256.Sum256(data)
	copy(hash, h[:])
	return hash
}

func computeSHA512(data []byte) []byte {
	hash := make([]byte, 64)
	h := sha512.Sum512(data)
	copy(hash, h[:])
	return hash
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
