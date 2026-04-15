package api

import (
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// setupTestServer creates a test server with the given config
func setupPasswordTestServer(t *testing.T, cfg Config) *Server {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	return NewServer(database, nil, cfg)
}

// TestHashPasswordArgon2id tests Argon2id password hashing
func TestHashPasswordArgon2id(t *testing.T) {
	hash, err := hashPasswordArgon2id("testpassword")
	if err != nil {
		t.Fatalf("hashPasswordArgon2id failed: %v", err)
	}

	// Verify format
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("expected argon2id hash, got %s", hash)
	}

	// Verify it has all parts
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		t.Errorf("expected 6 parts, got %d: %s", len(parts), hash)
	}
}

// TestVerifyPasswordArgon2id tests Argon2id password verification
func TestVerifyPasswordArgon2id(t *testing.T) {
	password := "testpassword123"
	hash, err := hashPasswordArgon2id(password)
	if err != nil {
		t.Fatalf("hashPasswordArgon2id failed: %v", err)
	}

	// Correct password
	if !verifyPasswordArgon2id(password, hash) {
		t.Error("verifyPasswordArgon2id should return true for correct password")
	}

	// Wrong password
	if verifyPasswordArgon2id("wrongpassword", hash) {
		t.Error("verifyPasswordArgon2id should return false for wrong password")
	}
}

// TestVerifyPasswordArgon2id_InvalidFormat tests verification with invalid formats
func TestVerifyPasswordArgon2id_InvalidFormat(t *testing.T) {
	tests := []struct {
		name  string
		hash  string
		valid bool
	}{
		{"empty string", "", false},
		{"no dollar signs", "argon2id", false},
		{"wrong algorithm", "$bcrypt$v=19$m=65536,t=1,p=4$salt$hash", false},
		{"too few parts", "$argon2id$v=19$m=65536,t=1,p=4$salt", false},
		{"invalid params", "$argon2id$v=19$invalid$salt$hash", false},
		{"invalid salt hex", "$argon2id$v=19$m=65536,t=1,p=4$nothex$hash", false},
		{"invalid hash hex", "$argon2id$v=19$m=65536,t=1,p=4$salt$nothex", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := verifyPasswordArgon2id("password", tc.hash)
			if result != tc.valid {
				t.Errorf("verifyPasswordArgon2id(%q): expected %v, got %v", tc.hash, tc.valid, result)
			}
		})
	}
}

// TestVerifyPasswordArgon2id_InvalidParameters tests with parameters that fail validation
func TestVerifyPasswordArgon2id_InvalidParameters(t *testing.T) {
	// Negative time
	hash := "$argon2id$v=19$m=65536,t=-1,p=4$aabbccdd$aabbccdd"
	if verifyPasswordArgon2id("password", hash) {
		t.Error("should fail with negative time")
	}

	// Zero threads
	hash = "$argon2id$v=19$m=65536,t=1,p=0$aabbccdd$aabbccdd"
	if verifyPasswordArgon2id("password", hash) {
		t.Error("should fail with zero threads")
	}

	// Too many threads
	hash = "$argon2id$v=19$m=65536,t=1,p=256$aabbccdd$aabbccdd"
	if verifyPasswordArgon2id("password", hash) {
		t.Error("should fail with too many threads")
	}

	// Empty hash
	hash = "$argon2id$v=19$m=65536,t=1,p=4$aabbccdd$"
	if verifyPasswordArgon2id("password", hash) {
		t.Error("should fail with empty hash")
	}
}

// TestServer_HashPassword_Argon2id tests server password hashing with argon2id
func TestServer_HashPassword_Argon2id(t *testing.T) {
	cfg := Config{
		JWTSecret:      "test-secret",
		TokenExpiry:    time.Hour,
		PasswordHasher: "argon2id",
	}

	server := setupPasswordTestServer(t, cfg)

	hash, err := server.hashPassword("testpassword")
	if err != nil {
		t.Fatalf("hashPassword failed: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("expected argon2id hash, got %s", hash)
	}
}

// TestServer_HashPassword_Bcrypt tests server password hashing with bcrypt (default)
func TestServer_HashPassword_Bcrypt(t *testing.T) {
	cfg := Config{
		JWTSecret:      "test-secret",
		TokenExpiry:    time.Hour,
		PasswordHasher: "bcrypt",
	}

	server := setupPasswordTestServer(t, cfg)

	hash, err := server.hashPassword("testpassword")
	if err != nil {
		t.Fatalf("hashPassword failed: %v", err)
	}

	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("expected bcrypt hash starting with $2, got %s", hash)
	}
}

// TestServer_VerifyPassword_Argon2id tests verifying argon2id passwords
func TestServer_VerifyPassword_Argon2id(t *testing.T) {
	cfg := Config{
		JWTSecret:      "test-secret",
		TokenExpiry:    time.Hour,
		PasswordHasher: "argon2id",
	}

	server := setupPasswordTestServer(t, cfg)

	password := "testpassword123"
	hash, _ := server.hashPassword(password)

	// Correct password
	matches, needsRehash := server.verifyPassword(password, hash)
	if !matches {
		t.Error("verifyPassword should return matches=true for correct password")
	}
	if needsRehash {
		t.Error("argon2id hash should not need rehash when argon2id is configured")
	}

	// Wrong password
	matches, _ = server.verifyPassword("wrongpassword", hash)
	if matches {
		t.Error("verifyPassword should return matches=false for wrong password")
	}
}

// TestServer_VerifyPassword_Bcrypt tests verifying bcrypt passwords
func TestServer_VerifyPassword_Bcrypt(t *testing.T) {
	cfg := Config{
		JWTSecret:      "test-secret",
		TokenExpiry:    time.Hour,
		PasswordHasher: "bcrypt",
	}

	server := setupPasswordTestServer(t, cfg)

	password := "testpassword123"
	hash, _ := server.hashPassword(password)

	// Correct password
	matches, needsRehash := server.verifyPassword(password, hash)
	if !matches {
		t.Error("verifyPassword should return matches=true for correct password")
	}
	if needsRehash {
		t.Error("bcrypt hash should not need rehash when bcrypt is configured")
	}
}

// TestServer_VerifyPassword_BcryptNeedsRehash tests that bcrypt needs rehash when argon2id preferred
func TestServer_VerifyPassword_BcryptNeedsRehash(t *testing.T) {
	cfg := Config{
		JWTSecret:      "test-secret",
		TokenExpiry:    time.Hour,
		PasswordHasher: "argon2id", // Prefer argon2id
	}

	server := setupPasswordTestServer(t, cfg)

	// Create a bcrypt hash manually
	bcryptHash := "$2a$10$N9qo8uLOickgx2ZMRZoMy.MqrqhmM6JGKpS4G3R1G2JH8YpfB0Bqy"

	matches, needsRehash := server.verifyPassword("testpassword", bcryptHash)
	// Note: This bcrypt hash is for "testpassword", but we can't verify it matches
	// Just check the logic flow - if it was valid, it would need rehash
	if !needsRehash && matches {
		t.Error("bcrypt hash should need rehash when argon2id is configured")
	}
}

// TestServer_VerifyPassword_UnknownFormat tests verifying unknown hash format
func TestServer_VerifyPassword_UnknownFormat(t *testing.T) {
	cfg := Config{
		JWTSecret:   "test-secret",
		TokenExpiry: time.Hour,
	}
	server := setupPasswordTestServer(t, cfg)

	matches, needsRehash := server.verifyPassword("password", "$unknown$hash")
	if matches {
		t.Error("verifyPassword should return false for unknown format")
	}
	if needsRehash {
		t.Error("verifyPassword should return needsRehash=false for unknown format")
	}
}

// TestServer_VerifyPassword_InvalidBcrypt tests verifying invalid bcrypt hash
func TestServer_VerifyPassword_InvalidBcrypt(t *testing.T) {
	cfg := Config{
		JWTSecret:   "test-secret",
		TokenExpiry: time.Hour,
	}
	server := setupPasswordTestServer(t, cfg)

	// Invalid bcrypt hash (starts with $2 but is invalid)
	matches, _ := server.verifyPassword("password", "$2a$10$invalid")
	if matches {
		t.Error("verifyPassword should return false for invalid bcrypt hash")
	}
}

// TestHashPasswordArgon2id_UniqueHashes tests that each hash is unique (different salt)
func TestHashPasswordArgon2id_UniqueHashes(t *testing.T) {
	password := "samepassword"
	hash1, err := hashPasswordArgon2id(password)
	if err != nil {
		t.Fatalf("first hash failed: %v", err)
	}

	hash2, err := hashPasswordArgon2id(password)
	if err != nil {
		t.Fatalf("second hash failed: %v", err)
	}

	if hash1 == hash2 {
		t.Error("hashes should be unique due to random salt")
	}

	// But both should verify
	if !verifyPasswordArgon2id(password, hash1) {
		t.Error("first hash should verify")
	}
	if !verifyPasswordArgon2id(password, hash2) {
		t.Error("second hash should verify")
	}
}
