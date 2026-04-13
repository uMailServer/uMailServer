package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// deriveTOTPKey derives a 32-byte AES key from the provided master key.
func deriveTOTPKey(masterKey string) []byte {
	sum := sha256.Sum256([]byte(masterKey))
	return sum[:]
}

// EncryptTOTPSecret encrypts a TOTP secret using AES-256-GCM.
// The ciphertext is returned as base64 with an "enc:" prefix.
// If masterKey is empty, the secret is returned unchanged.
func EncryptTOTPSecret(secret, masterKey string) (string, error) {
	if masterKey == "" || secret == "" {
		return secret, nil
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
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return "enc:" + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptTOTPSecret decrypts a TOTP secret encrypted with EncryptTOTPSecret.
// If the secret does not have the "enc:" prefix, it is returned unchanged.
// If masterKey is empty and the secret is encrypted, an error is returned.
func DecryptTOTPSecret(secret, masterKey string) (string, error) {
	if !strings.HasPrefix(secret, "enc:") {
		return secret, nil
	}
	if masterKey == "" {
		return "", fmt.Errorf("cannot decrypt TOTP secret: master key is empty")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(secret[4:])
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
