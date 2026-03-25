package auth

import (
	"crypto/rand"
	"crypto/rsa"
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
		name       string
		data       string
		wantUsage  TLSAUsage
		wantSelector TLSASelector
		wantMatch  TLSAMatchingType
		wantErr    bool
	}{
		{
			name:       "valid record - text format",
			data:       "3 1 1 " + hex.EncodeToString(hash),
			wantUsage:  TLSAUsageDANEEE,
			wantSelector: TLSASelectorSPKI,
			wantMatch:  TLSAMatchingTypeSHA256,
			wantErr:    false,
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
	data[0] = 3  // DANE-EE
	data[1] = 1  // SPKI
	data[2] = 1  // SHA-256
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
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"example.com"},
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
