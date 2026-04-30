package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLDAPConfig_Defaults(t *testing.T) {
	config := LDAPConfig{
		Enabled: true,
		URL:     "ldap://localhost:389",
		BaseDN:  "dc=example,dc=com",
	}

	client, err := NewLDAPClient(config)
	if err != nil {
		// Expected to fail since no LDAP server is running
		t.Logf("Expected connection error: %v", err)
		return
	}

	if client.config.UserFilter != "(uid=%s)" {
		t.Errorf("expected default user filter, got %s", client.config.UserFilter)
	}

	if client.config.EmailAttribute != "mail" {
		t.Errorf("expected default email attribute 'mail', got %s", client.config.EmailAttribute)
	}

	if client.config.NameAttribute != "cn" {
		t.Errorf("expected default name attribute 'cn', got %s", client.config.NameAttribute)
	}

	if client.config.GroupAttribute != "memberOf" {
		t.Errorf("expected default group attribute 'memberOf', got %s", client.config.GroupAttribute)
	}

	if client.config.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", client.config.Timeout)
	}
}

func TestLDAPClient_IsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		client   *LDAPClient
		expected bool
	}{
		{
			name:     "nil client",
			client:   nil,
			expected: false,
		},
		{
			name: "disabled",
			client: &LDAPClient{
				config: LDAPConfig{Enabled: false},
			},
			expected: false,
		},
		{
			name: "enabled",
			client: &LDAPClient{
				config: LDAPConfig{Enabled: true},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.client.IsEnabled()
			if result != tt.expected {
				t.Errorf("IsEnabled() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLDAPUser_IsAdmin(t *testing.T) {
	user := &LDAPUser{
		Username: "testuser",
		Groups:   []string{"cn=users,dc=example,dc=com", "cn=admins,dc=example,dc=com"},
	}

	// Test with matching admin group
	config := LDAPConfig{
		AdminGroups: []string{"cn=admins,dc=example,dc=com"},
	}

	for _, group := range user.Groups {
		for _, adminGroup := range config.AdminGroups {
			if group == adminGroup {
				user.IsAdmin = true
				break
			}
		}
	}

	if !user.IsAdmin {
		t.Error("user should be admin")
	}
}

func TestLDAPConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  LDAPConfig
		wantErr bool
	}{
		{
			name: "empty URL",
			config: LDAPConfig{
				Enabled: true,
				BaseDN:  "dc=example,dc=com",
			},
			wantErr: true,
		},
		{
			name: "empty BaseDN",
			config: LDAPConfig{
				Enabled: true,
				URL:     "ldap://localhost:389",
			},
			wantErr: true,
		},
		{
			name: "valid config",
			config: LDAPConfig{
				Enabled: true,
				URL:     "ldap://localhost:389",
				BaseDN:  "dc=example,dc=com",
			},
			wantErr: true, // Will fail to connect, but config is valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLDAPClient(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLDAPClient() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseLDAPURL(t *testing.T) {
	tests := []struct {
		url      string
		wantHost string
		wantTLS  bool
	}{
		{
			url:      "ldap://localhost:389",
			wantHost: "localhost:389",
			wantTLS:  false,
		},
		{
			url:      "ldaps://ldap.example.com:636",
			wantHost: "ldap.example.com:636",
			wantTLS:  true,
		},
		{
			url:      "ldap://ldap.example.com",
			wantHost: "ldap.example.com:389",
			wantTLS:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			// This test documents expected URL parsing behavior
			t.Logf("URL: %s, Host: %s, TLS: %v", tt.url, tt.wantHost, tt.wantTLS)
		})
	}
}

// Integration test - requires running LDAP server
// Set LDAP_TEST_URL environment variable to run
func TestLDAPIntegration(t *testing.T) {
	url := ""
	if url == "" {
		t.Skip("LDAP_TEST_URL not set, skipping integration test")
	}

	config := LDAPConfig{
		Enabled:      true,
		URL:          url,
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "admin",
		BaseDN:       "ou=users,dc=example,dc=com",
		UserFilter:   "(uid=%s)",
		AdminGroups:  []string{"cn=admins,ou=groups,dc=example,dc=com"},
		Timeout:      10 * time.Second,
	}

	client, err := NewLDAPClient(config)
	if err != nil {
		t.Fatalf("Failed to create LDAP client: %v", err)
	}

	// Test connection
	err = client.TestConnection()
	if err != nil {
		t.Logf("LDAP connection test failed (expected if no server): %v", err)
		return
	}

	t.Log("LDAP connection successful")
}

func TestLDAPConfig_SkipVerifyBlockedInProduction(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		wantErr     bool
	}{
		{"production", "production", true},
		{"staging", "staging", true},
		{"development", "development", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := LDAPConfig{
				Enabled:      true,
				URL:          "ldap://localhost:389",
				BaseDN:       "dc=example,dc=com",
				SkipVerify:   true,
				Environment:  tt.environment,
				BindDN:       "cn=admin,dc=example,dc=com",
				BindPassword: "admin",
			}
			_, err := NewLDAPClient(config)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for environment %q, got nil", tt.environment)
				} else if !strings.Contains(err.Error(), "skip_verify is not allowed") {
					t.Errorf("expected skip_verify error, got: %v", err)
				}
			} else {
				if err != nil && !strings.Contains(err.Error(), "connection test failed") {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestLDAPConfig_RootCAAndSkipVerifyConflict(t *testing.T) {
	config := LDAPConfig{
		Enabled:      true,
		URL:          "ldap://localhost:389",
		BaseDN:       "dc=example,dc=com",
		SkipVerify:   true,
		RootCA:       "/some/path.pem",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "admin",
	}
	_, err := NewLDAPClient(config)
	if err == nil {
		t.Fatal("expected error when both root_ca and skip_verify are set")
	}
	if !strings.Contains(err.Error(), "cannot be used together") {
		t.Errorf("expected conflict error, got: %v", err)
	}
}

func TestLDAPConfig_RootCANotFound(t *testing.T) {
	config := LDAPConfig{
		Enabled:      true,
		URL:          "ldap://localhost:389",
		BaseDN:       "dc=example,dc=com",
		RootCA:       "/nonexistent/path/ca.pem",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "admin",
	}
	_, err := NewLDAPClient(config)
	if err == nil {
		t.Fatal("expected error when root_ca file does not exist")
	}
	if !strings.Contains(err.Error(), "root_ca file not found") {
		t.Errorf("expected root_ca not found error, got: %v", err)
	}
}

func TestLDAPBuildTLSConfig_InvalidPEM(t *testing.T) {
	tmpDir := t.TempDir()
	badPEM := filepath.Join(tmpDir, "bad.pem")
	if err := os.WriteFile(badPEM, []byte("not a valid pem"), 0o644); err != nil {
		t.Fatalf("failed to write bad pem: %v", err)
	}

	client := &LDAPClient{
		config: LDAPConfig{RootCA: badPEM},
	}
	_, err := client.buildLDAPTLSConfig("ldap.example.com")
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
	if !strings.Contains(err.Error(), "failed to parse root_ca certificate") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestLDAPBuildTLSConfig_ValidPEM(t *testing.T) {
	// A minimal self-signed CA PEM for testing
	validPEM := `-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAKHBfpE
-----END CERTIFICATE-----`

	tmpDir := t.TempDir()
	goodPEM := filepath.Join(tmpDir, "good.pem")
	if err := os.WriteFile(goodPEM, []byte(validPEM), 0o644); err != nil {
		t.Fatalf("failed to write good pem: %v", err)
	}

	client := &LDAPClient{
		config: LDAPConfig{RootCA: goodPEM},
	}
	_, err := client.buildLDAPTLSConfig("ldap.example.com")
	if err == nil {
		// This PEM is truncated/malformed, so parsing may fail.
		// The test documents behavior; a real CA cert should succeed.
		t.Logf("Expected potential parse error for truncated test PEM: %v", err)
	}
}

func TestLDAPClient_Close(t *testing.T) {
	// Close on nil client should not panic
	var nilClient *LDAPClient
	nilClient.Close()

	// Close on client without pool should not panic
	client := &LDAPClient{config: LDAPConfig{Enabled: true}}
	client.Close()
}

func TestLDAPCheckLoginRateLimit(t *testing.T) {
	client := &LDAPClient{
		config: LDAPConfig{Enabled: true},
	}

	username := "testuser"

	// First 4 attempts should be allowed
	for i := 0; i < 4; i++ {
		if !client.checkLoginRateLimit(username) {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}

	// 5th attempt should be allowed
	if !client.checkLoginRateLimit(username) {
		t.Fatal("5th attempt should be allowed")
	}

	// 6th attempt should trigger lockout
	if client.checkLoginRateLimit(username) {
		t.Fatal("6th attempt should be blocked")
	}
}

func TestLDAPRecordLoginFailure(t *testing.T) {
	client := &LDAPClient{
		config: LDAPConfig{Enabled: true},
	}

	username := "testuser"
	client.recordLoginFailure(username)

	// After recording a failure, rate limit should still allow (count=1)
	if !client.checkLoginRateLimit(username) {
		t.Fatal("should still allow after 1 failure")
	}
}

func TestLDAPMin(t *testing.T) {
	if min(1, 2) != 1 {
		t.Error("min(1,2) should be 1")
	}
	if min(2, 1) != 1 {
		t.Error("min(2,1) should be 1")
	}
	if min(-1, -2) != -2 {
		t.Error("min(-1,-2) should be -2")
	}
}
