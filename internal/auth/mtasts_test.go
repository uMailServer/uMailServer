package auth

import (
	"context"
	"testing"
	"time"
)

func TestParseMTASTSRecord(t *testing.T) {
	tests := []struct {
		name       string
		record     string
		wantVersion string
		wantID     string
		wantErr    bool
	}{
		{
			name:       "valid record",
			record:     "v=STSv1; id=abc123",
			wantVersion: "STSv1",
			wantID:     "abc123",
			wantErr:    false,
		},
		{
			name:    "missing version",
			record:  "id=abc123",
			wantErr: true,
		},
		{
			name:    "missing id",
			record:  "v=STSv1",
			wantErr: true,
		},
		{
			name:    "wrong version",
			record:  "v=STSv2; id=abc123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := parseMTASTSRecord(tt.record)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if rec.Version != tt.wantVersion {
				t.Errorf("Version = %q, want %q", rec.Version, tt.wantVersion)
			}
			if rec.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", rec.ID, tt.wantID)
			}
		})
	}
}

func TestParseMTASTSPolicy(t *testing.T) {
	policyText := `version: STSv1
mode: enforce
max_age: 86400
mx: mail.example.com
mx: *.example.net`

	policy, err := parseMTASTSPolicy(policyText)
	if err != nil {
		t.Fatalf("parseMTASTSPolicy failed: %v", err)
	}

	if policy.Version != "STSv1" {
		t.Errorf("Version = %q, want STSv1", policy.Version)
	}

	if policy.Mode != MTASTSModeEnforce {
		t.Errorf("Mode = %q, want enforce", policy.Mode)
	}

	if policy.MaxAge != 86400 {
		t.Errorf("MaxAge = %d, want 86400", policy.MaxAge)
	}

	if len(policy.MX) != 2 {
		t.Errorf("MX count = %d, want 2", len(policy.MX))
	}
}

func TestParseMTASTSPolicyInvalidMode(t *testing.T) {
	policyText := `version: STSv1
mode: invalid
max_age: 86400`

	_, err := parseMTASTSPolicy(policyText)
	if err == nil {
		t.Error("Expected error for invalid mode")
	}
}

func TestParseMTASTSPolicyMinMaxAge(t *testing.T) {
	policyText := `version: STSv1
mode: enforce
max_age: 100`

	policy, err := parseMTASTSPolicy(policyText)
	if err != nil {
		t.Fatalf("parseMTASTSPolicy failed: %v", err)
	}

	// Should be clamped to minimum 86400
	if policy.MaxAge != 86400 {
		t.Errorf("MaxAge = %d, want 86400 (minimum)", policy.MaxAge)
	}
}

func TestMatchMX(t *testing.T) {
	tests := []struct {
		pattern  string
		mx       string
		expected bool
	}{
		// Exact match
		{"mail.example.com", "mail.example.com", true},
		{"mail.example.com", "mail.other.com", false},

		// Wildcard patterns
		{"*.example.com", "mail.example.com", true},
		{"*.example.com", "mx.example.com", true},
		{"*.example.com", "mail.other.com", false},
		{"*.example.com", "example.com", true}, // *.example.com matches example.com too

		// Case insensitivity
		{"MAIL.EXAMPLE.COM", "mail.example.com", true},
		{"*.EXAMPLE.COM", "mail.example.com", true},
	}

	for _, tt := range tests {
		got := matchMX(tt.pattern, tt.mx)
		if got != tt.expected {
			t.Errorf("matchMX(%q, %q) = %v, want %v", tt.pattern, tt.mx, got, tt.expected)
		}
	}
}

func TestComputePolicyID(t *testing.T) {
	policy1 := "version: STSv1\nmode: enforce"
	policy2 := "version: STSv1\nmode: testing"

	id1 := computePolicyID(policy1)
	id2 := computePolicyID(policy1)
	id3 := computePolicyID(policy2)

	// Same policy should produce same ID
	if id1 != id2 {
		t.Error("Same policy should produce same ID")
	}

	// Different policies should produce different IDs
	if id1 == id3 {
		t.Error("Different policies should produce different IDs")
	}

	// ID should be base64 encoded
	if len(id1) == 0 {
		t.Error("ID should not be empty")
	}
}

func TestNewMTASTSValidator(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	if validator == nil {
		t.Fatal("NewMTASTSValidator returned nil")
	}
	if validator.resolver != resolver {
		t.Error("Resolver not set correctly")
	}
	if validator.cache == nil {
		t.Error("Cache not initialized")
	}
	if validator.httpClient == nil {
		t.Error("HTTP client not initialized")
	}
}

func TestMTASTSValidatorCache(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	// Add a policy to cache
	validator.cacheMu.Lock()
	validator.cache["example.com"] = &MTASTSCacheEntry{
		Policy: &MTASTSPolicy{
			Version: "STSv1",
			Mode:    MTASTSModeEnforce,
			MX:      []string{"mail.example.com"},
			MaxAge:  86400,
		},
		Domain:    "example.com",
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	validator.cacheMu.Unlock()

	// Get policy (should hit cache)
	policy, err := validator.GetPolicy(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("GetPolicy failed: %v", err)
	}

	if policy == nil {
		t.Fatal("Policy should not be nil")
	}

	if policy.Mode != MTASTSModeEnforce {
		t.Errorf("Mode = %q, want enforce", policy.Mode)
	}
}

func TestMTASTSValidatorExpiredCache(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	// Add an expired policy to cache (no DNS record, so fetch should return no policy)
	validator.cacheMu.Lock()
	validator.cache["example.com"] = &MTASTSCacheEntry{
		Policy: &MTASTSPolicy{
			Version: "STSv1",
			Mode:    MTASTSModeEnforce,
		},
		Domain:    "example.com",
		FetchedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Hour), // Expired
	}
	validator.cacheMu.Unlock()

	// Get policy (should try to fetch but return no policy since no DNS record)
	policy, err := validator.GetPolicy(context.Background(), "example.com")
	// No error expected, but no policy either
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if policy != nil {
		t.Error("Expected nil policy when no DNS record exists")
	}
}

func TestMTASTSValidatorCacheStats(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	// Add some entries
	validator.cacheMu.Lock()
	validator.cache["example.com"] = &MTASTSCacheEntry{
		Policy:    &MTASTSPolicy{},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	validator.cache["expired.com"] = &MTASTSCacheEntry{
		Policy:    &MTASTSPolicy{},
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	validator.cacheMu.Unlock()

	total, expired := validator.GetCacheStats()

	if total != 2 {
		t.Errorf("Total = %d, want 2", total)
	}

	if expired != 1 {
		t.Errorf("Expired = %d, want 1", expired)
	}
}

func TestMTASTSValidatorClearCache(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	// Add an entry
	validator.cacheMu.Lock()
	validator.cache["example.com"] = &MTASTSCacheEntry{
		Policy:    &MTASTSPolicy{},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	validator.cacheMu.Unlock()

	// Clear cache
	validator.ClearCache()

	total, _ := validator.GetCacheStats()
	if total != 0 {
		t.Errorf("Total = %d after clear, want 0", total)
	}
}

func TestMTASTSPolicyCheck(t *testing.T) {
	policy := &MTASTSPolicy{
		Version: "STSv1",
		Mode:    MTASTSModeEnforce,
		MX:      []string{"mail.example.com", "*.backup.example.com"},
		MaxAge:  86400,
	}

	tests := []struct {
		mx       string
		expected bool
	}{
		{"mail.example.com", true},
		{"mx.backup.example.com", true},
		{"mail.other.com", false},
	}

	for _, tt := range tests {
		matched := false
		for _, pattern := range policy.MX {
			if matchMX(pattern, tt.mx) {
				matched = true
				break
			}
		}
		if matched != tt.expected {
			t.Errorf("MX check for %q: got %v, want %v", tt.mx, matched, tt.expected)
		}
	}
}

func TestMTASTSModeValues(t *testing.T) {
	if MTASTSModeEnforce != "enforce" {
		t.Error("MTASTSModeEnforce has wrong value")
	}
	if MTASTSModeTesting != "testing" {
		t.Error("MTASTSModeTesting has wrong value")
	}
	if MTASTSModeNone != "none" {
		t.Error("MTASTSModeNone has wrong value")
	}
}

func TestGenerateTLSRPT(t *testing.T) {
	failures := []MTASTSFailureDetails{
		{
			ResultType:       "certificate-host-mismatch",
			FailedSessionCount: 5,
		},
	}

	report := GenerateTLSRPT("example.com", failures)

	if report == "" {
		t.Error("Report should not be empty")
	}

	if !containsString(report, "example.com") {
		t.Error("Report should contain domain")
	}

	if !containsString(report, "certificate-host-mismatch") {
		t.Error("Report should contain failure type")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestCheckPolicy tests the CheckPolicy method
func TestCheckPolicy(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	// Add a policy to cache
	validator.cacheMu.Lock()
	validator.cache["example.com"] = &MTASTSCacheEntry{
		Policy: &MTASTSPolicy{
			Version: "STSv1",
			Mode:    MTASTSModeEnforce,
			MX:      []string{"mail.example.com", "*.backup.example.com"},
			MaxAge:  86400,
		},
		Domain:    "example.com",
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	validator.cacheMu.Unlock()

	tests := []struct {
		name          string
		domain        string
		mx            string
		expectedValid bool
		expectPolicy  bool
	}{
		{
			name:          "valid MX exact match",
			domain:        "example.com",
			mx:            "mail.example.com",
			expectedValid: true,
			expectPolicy:  true,
		},
		{
			name:          "valid MX wildcard match",
			domain:        "example.com",
			mx:            "mx.backup.example.com",
			expectedValid: true,
			expectPolicy:  true,
		},
		{
			name:          "invalid MX",
			domain:        "example.com",
			mx:            "mail.other.com",
			expectedValid: false,
			expectPolicy:  true,
		},
		{
			name:          "no policy domain",
			domain:        "nopolicy.com",
			mx:            "mail.nopolicy.com",
			expectedValid: true,
			expectPolicy:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, policy, err := validator.CheckPolicy(context.Background(), tt.domain, tt.mx)
			if err != nil {
				t.Fatalf("CheckPolicy failed: %v", err)
			}
			if valid != tt.expectedValid {
				t.Errorf("valid = %v, want %v", valid, tt.expectedValid)
			}
			if tt.expectPolicy && policy == nil {
				t.Error("expected policy, got nil")
			}
			if !tt.expectPolicy && policy != nil {
				t.Error("expected nil policy")
			}
		})
	}
}

// TestIsMTASTSEnforced tests the IsMTASTSEnforced method
func TestIsMTASTSEnforced(t *testing.T) {
	resolver := newMockDNSResolver()
	validator := NewMTASTSValidator(resolver)

	// Test with no policy
	enforced, err := validator.IsMTASTSEnforced(context.Background(), "nopolicy.com")
	if err != nil {
		t.Fatalf("IsMTASTSEnforced failed: %v", err)
	}
	if enforced {
		t.Error("expected no enforcement for domain without policy")
	}

	// Add an enforced policy to cache
	validator.cacheMu.Lock()
	validator.cache["enforced.com"] = &MTASTSCacheEntry{
		Policy: &MTASTSPolicy{
			Version: "STSv1",
			Mode:    MTASTSModeEnforce,
			MX:      []string{"mail.enforced.com"},
			MaxAge:  86400,
		},
		Domain:    "enforced.com",
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	// Add a testing mode policy
	validator.cache["testing.com"] = &MTASTSCacheEntry{
		Policy: &MTASTSPolicy{
			Version: "STSv1",
			Mode:    MTASTSModeTesting,
			MX:      []string{"mail.testing.com"},
			MaxAge:  86400,
		},
		Domain:    "testing.com",
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	validator.cacheMu.Unlock()

	// Test enforced policy
	enforced, err = validator.IsMTASTSEnforced(context.Background(), "enforced.com")
	if err != nil {
		t.Fatalf("IsMTASTSEnforced failed: %v", err)
	}
	if !enforced {
		t.Error("expected enforcement for domain with enforce policy")
	}

	// Test testing mode (not enforced)
	enforced, err = validator.IsMTASTSEnforced(context.Background(), "testing.com")
	if err != nil {
		t.Fatalf("IsMTASTSEnforced failed: %v", err)
	}
	if enforced {
		t.Error("expected no enforcement for domain with testing policy")
	}
}
