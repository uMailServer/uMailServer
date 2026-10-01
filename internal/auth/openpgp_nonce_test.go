package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
	"testing"
)

// TestEncryptContent_EncryptContentEmitsNoncePrefix pins the wire-format
// contract that DecryptMessage relies on.
//
// DecryptMessage (internal/auth/openpgp.go:228) reads the first
// gcm.NonceSize() bytes of the decoded body as the nonce. encryptContent's own
// comment states the same intent: "Prepend nonce to ciphertext (nonce is NOT
// included in Seal output with dst=nil)" -- but the code returned the Seal
// output directly and never prepended anything. gcm.Seal with dst=nil does not
// emit the nonce, so the nonce was generated, stashed in lastNonce, and then
// dropped: nothing in the produced message carried it.
func TestEncryptContent_EncryptContentEmitsNoncePrefix(t *testing.T) {
	enc := NewOpenPGPEncryptor([][]byte{[]byte("pub")})
	plaintext := []byte("attack at dawn")

	out, err := enc.encryptContent(plaintext)
	if err != nil {
		t.Fatalf("encryptContent: %v", err)
	}

	block, err := aes.NewCipher(enc.GetLastSessionKey())
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM: %v", err)
	}

	nonce := enc.GetLastNonce()
	if len(nonce) != gcm.NonceSize() {
		t.Fatalf("precondition: stored nonce is %d bytes, want %d", len(nonce), gcm.NonceSize())
	}

	if len(out) < gcm.NonceSize() {
		t.Fatalf("FAIL: encryptContent returned %d bytes, too short to carry a %d-byte nonce",
			len(out), gcm.NonceSize())
	}

	if !bytes.Equal(out[:gcm.NonceSize()], nonce) {
		t.Errorf("FAIL: encryptContent output does not begin with the nonce it generated.\n"+
			"  first %d bytes = %x\n  generated nonce = %x\n"+
			"  DecryptMessage (openpgp.go:228) reads the nonce from exactly this "+
			"position, and the encryptContent comment requires the nonce to be "+
			"prepended, but gcm.Seal(dst=nil) does not emit it",
			gcm.NonceSize(), out[:gcm.NonceSize()], nonce)
	}
}

// TestEncryptContent_Control_CryptoAndKeyAreSound is the control: it exercises
// the AES-GCM primitive and the encryptor's session key directly, with a nonce
// chosen here, and never touches encryptContent's output framing. It therefore
// passes both before and after the fix, proving the defect is solely the
// missing nonce prefix and not a broken cipher, key, or harness.
func TestEncryptContent_Control_CryptoAndKeyAreSound(t *testing.T) {
	enc := NewOpenPGPEncryptor([][]byte{[]byte("pub")})

	// Populate the session key via a real encryptContent call.
	if _, err := enc.encryptContent([]byte("seed")); err != nil {
		t.Fatalf("encryptContent: %v", err)
	}

	block, err := aes.NewCipher(enc.GetLastSessionKey())
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM: %v", err)
	}

	plaintext := []byte("attack at dawn")
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatalf("rand: %v", err)
	}

	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	got, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		t.Fatalf("control: direct GCM round-trip failed: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("control: round-trip = %q, want %q", got, plaintext)
	}
}
