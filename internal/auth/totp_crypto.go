package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// pbkdf2Iterations is the number of iterations for PBKDF2 key derivation.
	// 100,000 is the minimum recommended by OWASP for PBKDF2-HMAC-SHA256.
	pbkdf2Iterations = 100000
	// pbkdf2SaltLength is the length of the random salt in bytes.
	pbkdf2SaltLength = 16
	// pbkdf2KeyLength is the length of the derived AES key in bytes.
	pbkdf2KeyLength = 32
)

// deriveTOTPKey derives a 32-byte AES key from the provided master key using SHA-256.
// Deprecated: This is the legacy v1 derivation method. Use deriveTOTPKeyV2 for new secrets.
func deriveTOTPKey(masterKey string) []byte {
	sum := sha256.Sum256([]byte(masterKey))
	return sum[:]
}

// deriveTOTPKeyV2 derives a 32-byte AES key using PBKDF2-HMAC-SHA256.
func deriveTOTPKeyV2(masterKey string, salt []byte) []byte {
	return pbkdf2.Key([]byte(masterKey), salt, pbkdf2Iterations, pbkdf2KeyLength, sha256.New)
}

// EncryptTOTPSecret encrypts a TOTP secret using AES-256-GCM.
// The ciphertext is returned as base64 with an "enc2:" prefix (PBKDF2-derived key).
// If masterKey is empty, the secret is returned unchanged.
func EncryptTOTPSecret(secret, masterKey string) (string, error) {
	if masterKey == "" || secret == "" {
		return secret, nil
	}

	salt := make([]byte, pbkdf2SaltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	key := deriveTOTPKeyV2(masterKey, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(secret), nil)
	// Format: enc2:<base64(salt + ciphertext)>
	payload := append(salt, ciphertext...)
	return "enc2:" + base64.StdEncoding.EncodeToString(payload), nil
}

// DecryptTOTPSecret decrypts a TOTP secret encrypted with EncryptTOTPSecret.
// Supports both "enc2:" (PBKDF2) and "enc:" (legacy SHA-256) formats.
// If the secret does not have an "enc:" or "enc2:" prefix, it is returned unchanged.
// If masterKey is empty and the secret is encrypted, an error is returned.
func DecryptTOTPSecret(secret, masterKey string) (string, error) {
	if strings.HasPrefix(secret, "enc2:") {
		return decryptTOTPSecretV2(secret[5:], masterKey)
	}
	if strings.HasPrefix(secret, "enc:") {
		return decryptTOTPSecretV1(secret[4:], masterKey)
	}
	return secret, nil
}

func decryptTOTPSecretV2(payloadB64, masterKey string) (string, error) {
	if masterKey == "" {
		return "", fmt.Errorf("cannot decrypt TOTP secret: master key is empty")
	}
	payload, err := base64.StdEncoding.DecodeString(payloadB64)
	if err != nil {
		return "", fmt.Errorf("failed to decode payload: %w", err)
	}
	if len(payload) < pbkdf2SaltLength {
		return "", fmt.Errorf("payload too short")
	}
	salt := payload[:pbkdf2SaltLength]
	ciphertext := payload[pbkdf2SaltLength:]

	key := deriveTOTPKeyV2(masterKey, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}
	return string(plaintext), nil
}

func decryptTOTPSecretV1(ciphertextB64, masterKey string) (string, error) {
	if masterKey == "" {
		return "", fmt.Errorf("cannot decrypt TOTP secret: master key is empty")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}
	key := deriveTOTPKey(masterKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}
	return string(plaintext), nil
}
