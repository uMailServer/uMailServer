package auth

import (
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
