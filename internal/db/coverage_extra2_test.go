package db

import (
	"testing"
)

// TestSplitEmail exercises all branches of the unexported splitEmail function.
func TestSplitEmail(t *testing.T) {
	tests := []struct {
		name      string
		email     string
		wantLocal string
		wantDom   string
	}{
		{"normal address", "user@example.com", "user", "example.com"},
		{"no @ sign", "userexample", "userexample", ""},
		{"multiple @ signs uses last", "first@middle@domain.com", "first@middle", "domain.com"},
		{"empty string", "", "", ""},
		{"@ at start", "@domain.com", "", "domain.com"},
		{"@ at end", "user@", "user", ""},
		{"subaddressed", "user+tag@domain.com", "user+tag", "domain.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, domain := splitEmail(tt.email)
			if local != tt.wantLocal {
				t.Errorf("splitEmail(%q) local = %q, want %q", tt.email, local, tt.wantLocal)
			}
			if domain != tt.wantDom {
				t.Errorf("splitEmail(%q) domain = %q, want %q", tt.email, domain, tt.wantDom)
			}
		})
	}
}

// TestGetUserSecret verifies the GetUserSecret method uses splitEmail correctly.
func TestGetUserSecret(t *testing.T) {
	database := helperDB(t)

	// Create account with known password hash
	hash := "$argon2id$v=19$m=65536,t=3,p=2$abc"
	if err := database.CreateAccount(&AccountData{
		Email:        "testuser@example.com",
		LocalPart:    "testuser",
		Domain:       "example.com",
		PasswordHash: hash,
	}); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// GetUserSecret should find the account by email
	secret, err := database.GetUserSecret("testuser@example.com")
	if err != nil {
		t.Fatalf("GetUserSecret: %v", err)
	}
	if secret != hash {
		t.Errorf("expected hash %q, got %q", hash, secret)
	}

	// GetUserSecret with whitespace and mixed case should still work
	secret2, err := database.GetUserSecret("  TestUser@Example.Com  ")
	if err != nil {
		t.Fatalf("GetUserSecret (normalized): %v", err)
	}
	if secret2 != hash {
		t.Errorf("expected hash %q, got %q", hash, secret2)
	}

	// GetUserSecret for non-existent user
	_, err = database.GetUserSecret("nobody@nowhere.com")
	if err == nil {
		t.Error("expected error for non-existent user")
	}
}
