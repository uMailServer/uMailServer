package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"testing"
)

// TestDecryptMessage_ReadsCiphertextPart pins which multipart part
// DecryptMessage must read.
//
// RFC 3156 multipart/encrypted carries two parts:
//  1. application/pgp-encrypted  -> the control part, body "Version: 1"
//  2. application/octet-stream   -> base64 of the encrypted session key +
//     ciphertext
//
// parseMultipart returns the parts in document order, so the ciphertext is
// parts[1]. Reading parts[0] base64-decodes the literal "Version: 1", which
// is not valid base64, so every OpenPGP decryption failed before the fix.
func TestDecryptMessage_ReadsCiphertextPart(t *testing.T) {
	plaintext := []byte("Subject: hello\r\n\r\nattack at dawn")

	enc := NewOpenPGPEncryptor([][]byte{[]byte("public-key-material")})
	encMsg, err := enc.EncryptMessage(plaintext, "alice@example.com", "bob@example.com")
	if err != nil {
		t.Fatalf("EncryptMessage: %v", err)
	}

	// Control: confirm the producer really does put the ciphertext in part 2,
	// so a failure below cannot be blamed on a wrong premise about the format.
	parts, err := parseMultipart(encMsg)
	if err != nil {
		t.Fatalf("control: parseMultipart: %v", err)
	}
	if len(parts) < 2 {
		t.Fatalf("control precondition: want >= 2 parts, got %d", len(parts))
	}
	if !bytes.Contains(parts[0].Content, []byte("Version")) {
		t.Errorf("control: parts[0] should be the pgp-encrypted control part, got %q", parts[0].Content)
	}
	if bytes.Contains(parts[1].Content, []byte("Version")) {
		t.Errorf("control: parts[1] should hold ciphertext, got %q", parts[1].Content)
	}

	dec := NewOpenPGPDecryptor([]byte("private-key-material"))
	dec.SetLastSessionKeyAndNonce(enc.GetLastSessionKey(), enc.GetLastNonce())

	got, err := dec.DecryptMessage(encMsg)
	if err != nil {
		t.Fatalf("DecryptMessage(EncryptMessage(x)) failed: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round-trip returned %q, want %q", got, plaintext)
	}
}

// TestDecryptMessage_Control_CryptoFramingIsSound is the framing-agnostic
// control: it performs the decryptor's crypto directly on the encryptor's
// bytes, bypassing multipart parsing entirely. It must pass regardless of which
// part the parser hands over, so it can never be the reason the round-trip
// above fails.
func TestDecryptMessage_Control_CryptoFramingIsSound(t *testing.T) {
	plaintext := []byte("attack at dawn")

	enc := NewOpenPGPEncryptor([][]byte{[]byte("public-key-material")})
	ciphertext, err := enc.encryptContent(plaintext)
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

	got, err := gcm.Open(nil, nonce, ciphertext[gcm.NonceSize():], nil)
	if err != nil {
		t.Fatalf("control: crypto round-trip failed: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("control: crypto round-trip returned %q, want %q", got, plaintext)
	}
}
