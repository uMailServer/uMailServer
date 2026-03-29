package auth

import (
	"crypto/hmac"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
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

// TestNewUserAuthenticator tests creating a new authenticator
func TestNewUserAuthenticator(t *testing.T) {
	getUser := func(email string) (*UserData, error) {
		return nil, ErrUserNotFound
	}

	auth := NewUserAuthenticator(getUser)
	if auth == nil {
		t.Fatal("NewUserAuthenticator returned nil")
	}
	if auth.GetUser == nil {
		t.Error("GetUser function not set")
	}
}

// TestUserAuthenticatorAuthenticate tests authentication
func TestUserAuthenticatorAuthenticate(t *testing.T) {
	// Create a test user with hashed password
	testPassword := "testpassword123"
	passwordHash, _ := HashPassword(testPassword)

	getUser := func(email string) (*UserData, error) {
		if email == "test@example.com" {
			return &UserData{
				Email:        "test@example.com",
				PasswordHash: passwordHash,
				Domain:       "example.com",
				IsActive:     true,
			}, nil
		}
		return nil, ErrUserNotFound
	}

	auth := NewUserAuthenticator(getUser)

	// Test successful authentication
	user, err := auth.Authenticate("test@example.com", testPassword)
	if err != nil {
		t.Errorf("Authentication failed: %v", err)
	}
	if user == nil {
		t.Error("Expected user data, got nil")
	}

	// Test wrong password
	user, err = auth.Authenticate("test@example.com", "wrongpassword")
	if err != ErrInvalidCredentials {
		t.Errorf("Expected ErrInvalidCredentials, got: %v", err)
	}

	// Test non-existent user
	user, err = auth.Authenticate("nonexistent@example.com", testPassword)
	if err != ErrUserNotFound {
		t.Errorf("Expected ErrUserNotFound, got: %v", err)
	}
}

// TestUserAuthenticatorAuthenticateInactiveUser tests authentication with inactive user
func TestUserAuthenticatorAuthenticateInactiveUser(t *testing.T) {
	testPassword := "testpassword123"
	passwordHash, _ := HashPassword(testPassword)

	getUser := func(email string) (*UserData, error) {
		if email == "inactive@example.com" {
			return &UserData{
				Email:        "inactive@example.com",
				PasswordHash: passwordHash,
				Domain:       "example.com",
				IsActive:     false,
			}, nil
		}
		return nil, ErrUserNotFound
	}

	auth := NewUserAuthenticator(getUser)

	// Test inactive user
	user, err := auth.Authenticate("inactive@example.com", testPassword)
	if err != ErrUserDisabled {
		t.Errorf("Expected ErrUserDisabled, got: %v", err)
	}
	if user != nil {
		t.Error("Expected nil user for inactive account")
	}
}

// TestVerifyCRAMMD5 tests CRAM-MD5 verification
func TestVerifyCRAMMD5(t *testing.T) {
	// Create a challenge and expected response
	challenge := "PDE3OTQxNjE0MDRAaGVsbG9wb3J0LmV4YW1wbGUuY29tPg=="
	secret := "testsecret"
	username := "test@example.com"

	// Calculate expected HMAC
	challengeBytes, _ := base64.StdEncoding.DecodeString(challenge)
	h := hmac.New(md5.New, []byte(secret))
	h.Write(challengeBytes)
	expectedHMAC := hex.EncodeToString(h.Sum(nil))

	// Create valid response: base64(username + " " + hex(hmac))
	response := base64.StdEncoding.EncodeToString([]byte(username + " " + expectedHMAC))

	// Test valid response
	user, ok := VerifyCRAMMD5(challenge, response, func(u string) (string, error) {
		if u == username {
			return secret, nil
		}
		return "", ErrUserNotFound
	})

	if !ok {
		t.Error("Expected CRAM-MD5 verification to succeed")
	}
	if user != username {
		t.Errorf("Expected username %s, got %s", username, user)
	}
}

// TestVerifyCRAMMD5InvalidResponse tests CRAM-MD5 with invalid response format
func TestVerifyCRAMMD5InvalidResponse(t *testing.T) {
	challenge := "PDE3OTQxNjE0MDRAaGVsbG9wb3J0LmV4YW1wbGUuY29tPg=="

	// Test with non-base64 response
	_, ok := VerifyCRAMMD5(challenge, "invalid-base64", func(u string) (string, error) {
		return "secret", nil
	})
	if ok {
		t.Error("Expected CRAM-MD5 verification to fail with invalid base64")
	}

	// Test with response without space separator
	response := base64.StdEncoding.EncodeToString([]byte("nospace"))
	_, ok = VerifyCRAMMD5(challenge, response, func(u string) (string, error) {
		return "secret", nil
	})
	if ok {
		t.Error("Expected CRAM-MD5 verification to fail without space separator")
	}
}

// TestVerifyCRAMMD5WrongSecret tests CRAM-MD5 with incorrect secret
func TestVerifyCRAMMD5WrongSecret(t *testing.T) {
	challenge := "PDE3OTQxNjE0MDRAaGVsbG9wb3J0LmV4YW1wbGUuY29tPg=="
	secret := "correctsecret"
	username := "test@example.com"

	// Calculate expected HMAC with correct secret
	challengeBytes, _ := base64.StdEncoding.DecodeString(challenge)
	h := hmac.New(md5.New, []byte(secret))
	h.Write(challengeBytes)
	expectedHMAC := hex.EncodeToString(h.Sum(nil))

	// Create valid response
	response := base64.StdEncoding.EncodeToString([]byte(username + " " + expectedHMAC))

	// Verify with wrong secret
	user, ok := VerifyCRAMMD5(challenge, response, func(u string) (string, error) {
		return "wrongsecret", nil // Return wrong secret
	})

	if ok {
		t.Error("Expected CRAM-MD5 verification to fail with wrong secret")
	}
	if user != username {
		t.Errorf("Expected username to be returned even on failure, got %s", user)
	}
}

// TestVerifyCRAMMD5MissingUser tests CRAM-MD5 when user is not found
func TestVerifyCRAMMD5MissingUser(t *testing.T) {
	challenge := "PDE3OTQxNjE0MDRAaGVsbG9wb3J0LmV4YW1wbGUuY29tPg=="
	response := base64.StdEncoding.EncodeToString([]byte("unknown@example.com somedigest"))

	_, ok := VerifyCRAMMD5(challenge, response, func(u string) (string, error) {
		return "", ErrUserNotFound
	})

	if ok {
		t.Error("Expected CRAM-MD5 verification to fail when user not found")
	}
}
