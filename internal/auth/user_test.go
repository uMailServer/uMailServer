package auth

import (
	"strings"
	"testing"
)

func TestHashPassword(t *testing.T) {
	password := "testpassword123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	// Hash should be non-empty
	if hash == "" {
		t.Error("Expected non-empty hash")
	}

	// Should start with $argon2id$
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("Expected hash to start with $argon2id$, got: %s", hash)
	}

	// Verify correct password
	if !VerifyPassword(password, hash) {
		t.Error("VerifyPassword should return true for correct password")
	}

	// Verify wrong password
	if VerifyPassword("wrongpassword", hash) {
		t.Error("VerifyPassword should return false for wrong password")
	}
}

func TestUserAuthenticator(t *testing.T) {
	// Create a test user
	password := "testpass123"
	hash, _ := HashPassword(password)

	testUser := &UserData{
		Email:        "test@example.com",
		PasswordHash: hash,
		Domain:       "example.com",
		IsActive:     true,
	}

	// Create authenticator with mock GetUser
	auth := NewUserAuthenticator(func(email string) (*UserData, error) {
		if email == "test@example.com" {
			return testUser, nil
		}
		return nil, ErrUserNotFound
	})

	t.Run("ValidCredentials", func(t *testing.T) {
		user, err := auth.Authenticate("test@example.com", password)
		if err != nil {
			t.Fatalf("Authentication failed: %v", err)
		}
		if user.Email != "test@example.com" {
			t.Errorf("Expected email test@example.com, got %s", user.Email)
		}
	})

	t.Run("InvalidPassword", func(t *testing.T) {
		_, err := auth.Authenticate("test@example.com", "wrongpassword")
		if err != ErrInvalidCredentials {
			t.Errorf("Expected ErrInvalidCredentials, got %v", err)
		}
	})

	t.Run("UserNotFound", func(t *testing.T) {
		_, err := auth.Authenticate("nonexistent@example.com", password)
		if err != ErrUserNotFound {
			t.Errorf("Expected ErrUserNotFound, got %v", err)
		}
	})

	t.Run("UserDisabled", func(t *testing.T) {
		// Create disabled user
		disabledUser := &UserData{
			Email:        "disabled@example.com",
			PasswordHash: hash,
			IsActive:     false,
		}

		auth2 := NewUserAuthenticator(func(email string) (*UserData, error) {
			if email == "disabled@example.com" {
				return disabledUser, nil
			}
			return nil, ErrUserNotFound
		})

		_, err := auth2.Authenticate("disabled@example.com", password)
		if err != ErrUserDisabled {
			t.Errorf("Expected ErrUserDisabled, got %v", err)
		}
	})

	t.Run("CaseInsensitive", func(t *testing.T) {
		user, err := auth.Authenticate("TEST@EXAMPLE.COM", password)
		if err != nil {
			t.Fatalf("Authentication failed: %v", err)
		}
		if user.Email != "test@example.com" {
			t.Errorf("Expected email test@example.com, got %s", user.Email)
		}
	})
}

func TestBcryptBackwardCompatibility(t *testing.T) {
	password := "testpassword123"

	// Create bcrypt hash
	hash, err := HashPasswordBcrypt(password)
	if err != nil {
		t.Fatalf("HashPasswordBcrypt failed: %v", err)
	}

	// Verify with VerifyPassword (should detect bcrypt and verify)
	if !VerifyPassword(password, hash) {
		t.Error("VerifyPassword should handle bcrypt hashes")
	}

	// Wrong password should fail
	if VerifyPassword("wrongpassword", hash) {
		t.Error("VerifyPassword should return false for wrong password")
	}
}

func TestGenerateAppPassword(t *testing.T) {
	password, err := GenerateAppPassword()
	if err != nil {
		t.Fatalf("GenerateAppPassword failed: %v", err)
	}

	// Should be non-empty
	if password == "" {
		t.Error("Expected non-empty password")
	}

	// Should match format: xxxx-xxxx-xxxx-xxxx
	parts := strings.Split(password, "-")
	if len(parts) != 4 {
		t.Errorf("Expected 4 parts, got %d", len(parts))
	}

	// Each part should be 8 hex characters
	for i, part := range parts {
		if len(part) != 8 {
			t.Errorf("Part %d should be 8 characters, got %d", i, len(part))
		}
	}
}

func TestGenerateCRAMMD5Challenge(t *testing.T) {
	challenge, challengeB64, err := GenerateCRAMMD5Challenge()
	if err != nil {
		t.Fatalf("GenerateCRAMMD5Challenge failed: %v", err)
	}

	// Challenge should be non-empty
	if challenge == "" {
		t.Error("Expected non-empty challenge")
	}

	// Challenge should start with < and end with @umailserver>
	if !strings.HasPrefix(challenge, "<") {
		t.Error("Expected challenge to start with <")
	}
	if !strings.HasSuffix(challenge, "@umailserver>") {
		t.Error("Expected challenge to end with @umailserver>")
	}

	// Base64 version should be non-empty
	if challengeB64 == "" {
		t.Error("Expected non-empty base64 challenge")
	}
}
