package smtp

import (
	"testing"
)

// --- OpenPGPStage SignMessage/EncryptMessage tests ---

func TestOpenPGPStage_SignMessage_NoKeystore(t *testing.T) {
	stage := NewOpenPGPStage(nil)

	_, err := stage.SignMessage("user", "from@test.com", "to@test.com", []byte("data"))
	if err == nil {
		t.Error("Expected error when keystore is nil")
	}
}

func TestOpenPGPStage_EncryptMessage_NoKeystore(t *testing.T) {
	stage := NewOpenPGPStage(nil)

	_, err := stage.EncryptMessage("user", "from@test.com", "to@test.com", []byte("data"))
	if err == nil {
		t.Error("Expected error when keystore is nil")
	}
}

func TestOpenPGPStage_SignMessage_NoKeys(t *testing.T) {
	ks := NewOpenPGPKeystore()
	stage := NewOpenPGPStage(ks)

	_, err := stage.SignMessage("nonexistent", "from@test.com", "to@test.com", []byte("data"))
	if err == nil {
		t.Error("Expected error when user has no keys")
	}
}

func TestOpenPGPStage_EncryptMessage_NoKeys(t *testing.T) {
	ks := NewOpenPGPKeystore()
	stage := NewOpenPGPStage(ks)

	_, err := stage.EncryptMessage("nonexistent", "from@test.com", "to@test.com", []byte("data"))
	if err == nil {
		t.Error("Expected error when user has no keys")
	}
}
