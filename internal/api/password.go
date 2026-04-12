package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Argon2id password hashing parameters (OWASP recommended)
const (
	argon2Time    = 1
	argon2Memory  = 64 * 1024 // 64 MB
	argon2Threads = 4
	argon2KeyLen  = 32
)

// hashPasswordArgon2id hashes a password using Argon2id
func hashPasswordArgon2id(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	// Format: $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Threads,
		hex.EncodeToString(salt), hex.EncodeToString(hash))
	return encoded, nil
}

// verifyPassword verifies a password against an Argon2id hash
func verifyPasswordArgon2id(password, encodedHash string) bool {
	// Parse the hash format
	// $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false
	}
	memoryStr := parts[3] // m=65536,t=1,p=4
	saltHex := parts[4]
	hashHex := parts[5]

	// Parse memory, time, threads from memoryStr
	var memory, time, threads int
	if _, err := fmt.Sscanf(memoryStr, "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}

	// Decode salt and stored hash
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	storedHash, err := hex.DecodeString(hashHex)
	if err != nil {
		return false
	}

	// Compute hash with same parameters
	computedHash := argon2.IDKey([]byte(password), salt, uint32(time), uint32(memory), uint8(threads), uint32(len(storedHash)))

	// Constant-time comparison
	if len(computedHash) != len(storedHash) {
		return false
	}
	var result byte
	for i := range computedHash {
		result |= computedHash[i] ^ storedHash[i]
	}
	return result == 0
}

// hashPassword hashes a password using the configured hasher
func (s *Server) hashPassword(password string) (string, error) {
	if s.config.PasswordHasher == "argon2id" {
		return hashPasswordArgon2id(password)
	}
	// Default to bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// verifyPassword verifies a password against a stored hash
// Returns (matches, needsRehash) where needsRehash is true if the hash uses an older algorithm
func (s *Server) verifyPassword(password, encodedHash string) (bool, bool) {
	// Try bcrypt first (legacy)
	if strings.HasPrefix(encodedHash, "$2") {
		err := bcrypt.CompareHashAndPassword([]byte(encodedHash), []byte(password))
		if err == nil {
			// If using bcrypt but argon2id is preferred, needs rehash
			return true, s.config.PasswordHasher == "argon2id"
		}
		return false, false
	}
	// Try argon2id
	if strings.HasPrefix(encodedHash, "$argon2id$") {
		if verifyPasswordArgon2id(password, encodedHash) {
			// Already using argon2id, no rehash needed
			return true, false
		}
		return false, false
	}
	// Unknown format
	return false, false
}
