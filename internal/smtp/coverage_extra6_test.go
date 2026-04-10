package smtp

import (
	"testing"
)

// --- OpenPGPStage coverage ---

func TestOpenPGPStage_NewOpenPGPStage(t *testing.T) {
	keystore := NewOpenPGPKeystore()
	stage := NewOpenPGPStage(keystore)

	if stage == nil {
		t.Fatal("Expected non-nil stage")
	}
	if stage.keystore != keystore {
		t.Error("Expected keystore to be set")
	}
}

func TestOpenPGPStage_NewOpenPGPKeystore(t *testing.T) {
	ks := NewOpenPGPKeystore()

	if ks == nil {
		t.Fatal("Expected non-nil keystore")
	}
	if ks.users == nil {
		t.Error("Expected users map to be initialized")
	}
}

func TestOpenPGPKeystore_GetKeys_NoUser(t *testing.T) {
	ks := NewOpenPGPKeystore()

	keys := ks.GetKeys("nonexistent")
	if keys != nil {
		t.Error("Expected nil for nonexistent user")
	}
}

func TestOpenPGPKeystore_GetKeys_NilKeystore(t *testing.T) {
	var ks *OpenPGPKeystore

	keys := ks.GetKeys("anyuser")
	if keys != nil {
		t.Error("Expected nil for nil keystore")
	}
}

func TestOpenPGPKeystore_SetKeys(t *testing.T) {
	ks := NewOpenPGPKeystore()
	userKeys := &OpenPGPUserKeys{
		PrivateKey: []byte("private"),
		PublicKey:  []byte("public"),
	}

	ks.SetKeys("testuser", userKeys)

	retrieved := ks.GetKeys("testuser")
	if retrieved == nil {
		t.Fatal("Expected non-nil keys after SetKeys")
	}
	if string(retrieved.PrivateKey) != "private" {
		t.Errorf("Expected private key 'private', got %q", string(retrieved.PrivateKey))
	}
}

func TestOpenPGPKeystore_SetKeys_NilKeystore(t *testing.T) {
	var ks *OpenPGPKeystore
	userKeys := &OpenPGPUserKeys{
		PrivateKey: []byte("private"),
		PublicKey:  []byte("public"),
	}

	// Should not panic
	ks.SetKeys("testuser", userKeys)
}

func TestOpenPGPStage_Name(t *testing.T) {
	stage := NewOpenPGPStage(nil)

	if stage.Name() != "OpenPGP" {
		t.Errorf("Expected name 'OpenPGP', got %q", stage.Name())
	}
}

func TestOpenPGPStage_Process_NoOpenPGPContent(t *testing.T) {
	stage := NewOpenPGPStage(nil)
	ctx := &MessageContext{
		Headers: map[string][]string{
			"Content-Type": {"text/plain"},
		},
	}

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for non-OpenPGP content, got %v", result)
	}
}

func TestOpenPGPStage_Process_WithPGPSignature(t *testing.T) {
	stage := NewOpenPGPStage(nil)
	ctx := &MessageContext{
		Headers: map[string][]string{
			"Content-Type": {"multipart/signed; protocol=\"application/pgp-signature\""},
		},
		Data: []byte("test message"),
	}

	// This will try to verify but we don't have real keys
	result := stage.Process(ctx)
	// Should return ResultAccept even on verification failure (fail-open for signatures)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}

func TestOpenPGPStage_Process_WithPGPEncrypted(t *testing.T) {
	stage := NewOpenPGPStage(nil)
	ctx := &MessageContext{
		Headers: map[string][]string{
			"Content-Type": {"multipart/encrypted; protocol=\"application/pgp-encrypted\""},
		},
		Data: []byte("encrypted content"),
	}

	// This will try to decrypt but we don't have real keys
	result := stage.Process(ctx)
	// Should return ResultAccept even on decryption failure
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}

func TestOpenPGPStage_Process_WithKeystore(t *testing.T) {
	ks := NewOpenPGPKeystore()
	ks.SetKeys("testuser", &OpenPGPUserKeys{
		PrivateKey: []byte("test-private-key"),
		PublicKey:  []byte("test-public-key"),
	})

	stage := NewOpenPGPStage(ks)
	ctx := &MessageContext{
		Headers: map[string][]string{
			"Content-Type": {"text/plain"},
		},
	}

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}
