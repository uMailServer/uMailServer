package auth

import (
	"testing"
)

func TestOpenPGPSigner(t *testing.T) {
	// Create signer without keys
	signer := NewOpenPGPSigner(nil)
	if signer == nil {
		t.Fatal("Expected non-nil signer")
	}

	// Try to sign without keys - should fail
	_, err := signer.SignMessage([]byte("test message"), "from@example.com", "to@example.com")
	if err == nil {
		t.Error("Expected error when signing without keys")
	}
}

func TestOpenPGPVerifier(t *testing.T) {
	// Create verifier without keys
	verifier := NewOpenPGPVerifier(nil)
	if verifier == nil {
		t.Fatal("Expected non-nil verifier")
	}

	// Try to verify without keys - should fail
	_, err := verifier.VerifyMessage([]byte("test message"))
	if err == nil {
		t.Error("Expected error when verifying without keys")
	}
}

func TestOpenPGPEncryptor(t *testing.T) {
	// Create encryptor without keys
	encryptor := NewOpenPGPEncryptor(nil)
	if encryptor == nil {
		t.Fatal("Expected non-nil encryptor")
	}

	// Try to encrypt without keys - should fail
	_, err := encryptor.EncryptMessage([]byte("test message"), "from@example.com", "to@example.com")
	if err == nil {
		t.Error("Expected error when encrypting without keys")
	}
}

func TestOpenPGPDecryptor(t *testing.T) {
	// Create decryptor without keys
	decryptor := NewOpenPGPDecryptor(nil)
	if decryptor == nil {
		t.Fatal("Expected non-nil decryptor")
	}

	// Try to decrypt without keys - should fail
	_, err := decryptor.DecryptMessage([]byte("test message"))
	if err == nil {
		t.Error("Expected error when decrypting without keys")
	}
}
