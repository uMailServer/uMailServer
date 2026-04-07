package auth

import (
	"testing"
)

func TestSMIMEConfig(t *testing.T) {
	cfg := NewSMIMEConfig()
	if cfg == nil {
		t.Fatal("Expected non-nil config")
	}
	if cfg.RequireEncryption {
		t.Error("RequireEncryption should be false by default")
	}
}

func TestSMIMESigner(t *testing.T) {
	// Create a test config
	testCfg := &SMIMEConfig{}

	// Create signer without keys
	signer := NewSMIMESigner(testCfg)
	if signer == nil {
		t.Fatal("Expected non-nil signer")
	}

	// Try to sign without keys - should fail
	_, err := signer.SignMessage([]byte("test message"), "from@example.com", "to@example.com")
	if err == nil {
		t.Error("Expected error when signing without keys")
	}
}

func TestSMIMEEncryptor(t *testing.T) {
	// Create encryptor without certificates
	encryptor := NewSMIMEEncryptor(&SMIMEConfig{})
	if encryptor == nil {
		t.Fatal("Expected non-nil encryptor")
	}

	// Try to encrypt without certificates - should fail
	_, err := encryptor.EncryptMessage([]byte("test message"), "from@example.com", "to@example.com")
	if err == nil {
		t.Error("Expected error when encrypting without certificates")
	}
}

