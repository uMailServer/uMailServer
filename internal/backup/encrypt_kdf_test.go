package backup

// Regression tests for the backup encryption key-derivation defect: Encrypt
// derived the AES-256 key from raw SHA-256(password) — the per-file 16-byte
// salt it generated and wrote to the file was never mixed into the key — so
// every backup sharing a password shared the exact same AES key
// (precomputation/rainbow-table friendly, cross-backup linkage, one crack
// decrypts them all). Fix: versioned header ("BK" + 0x02) with a per-file salt
// and PBKDF2-HMAC-SHA256; Decrypt auto-detects legacy (v1) files and keeps
// decrypting them with the old derivation.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

// kdfIterations mirrors the production PBKDF2 iteration count; the salting
// property under test is independent of the exact count.
const kdfIterations = 600000

func encryptFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, []byte("backup payload for round 19"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// readEnvelope parses the on-disk envelope and returns the raw framing so the
// test can derive keys independently of Decrypt.
func readEnvelope(t *testing.T, path string) (marker string, version byte, salt, nonce, ciphertext []byte) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(raw) < 3 {
		t.Fatalf("envelope too short: %d bytes", len(raw))
	}
	if string(raw[:2]) == "BK" {
		version = raw[2]
		salt = raw[3:19]
		nonce = raw[19:31]
		return "BK", version, salt, nonce, raw[31:]
	}
	// Legacy v1 framing: salt(16) + nonce(12) + ciphertext.
	salt = raw[:16]
	nonce = raw[16:28]
	return "", 0, salt, nonce, raw[28:]
}

func manualDecrypt(t *testing.T, key, nonce, ciphertext []byte) ([]byte, error) {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func TestEncryptKeyDerivationIsSalted(t *testing.T) {
	m := NewManager(t.TempDir(), nil, nil)
	src := encryptFixture(t)
	enc1 := filepath.Join(t.TempDir(), "a.enc")
	enc2 := filepath.Join(t.TempDir(), "b.enc")

	if err := m.Encrypt(src, enc1, "correct horse"); err != nil {
		t.Fatalf("encrypt 1: %v", err)
	}
	if err := m.Encrypt(src, enc2, "correct horse"); err != nil {
		t.Fatalf("encrypt 2: %v", err)
	}

	// The v2 envelope must carry the version marker.
	marker, version, salt, nonce, ct := readEnvelope(t, enc1)
	if marker != "BK" || version != 0x02 {
		t.Fatalf("FAIL: envelope is not versioned v2 (marker %q version %#x) — the salt cannot be wired into the KDF compatibly", marker, version)
	}
	if len(salt) != 16 {
		t.Fatalf("FAIL: salt length %d, want 16", len(salt))
	}

	// The defect: the key must NOT be the unsalted SHA-256(password) anymore.
	legacyKey := sha256.Sum256([]byte("correct horse"))
	if _, err := manualDecrypt(t, legacyKey[:], nonce, ct); err == nil {
		t.Fatalf("FAIL: ciphertext decrypts with unsalted sha256(password) — the salt is not mixed into the key")
	}

	// The fix: the stored salt, via PBKDF2, must derive the real key.
	saltedKey := pbkdf2.Key([]byte("correct horse"), salt, kdfIterations, 32, sha256.New)
	plain, err := manualDecrypt(t, saltedKey, nonce, ct)
	if err != nil {
		t.Fatalf("FAIL: PBKDF2(salt) key does not decrypt the envelope: %v", err)
	}
	if string(plain) != "backup payload for round 19" {
		t.Fatalf("FAIL: decrypted payload %q", plain)
	}

	// Two encryptions of the same plaintext must derive independent keys:
	// decrypting envelope 2 with envelope 1's derived key must fail.
	_, _, salt2, nonce2, ct2 := readEnvelope(t, enc2)
	if bytes.Equal(salt, salt2) {
		t.Fatalf("FAIL: salt reused across encryptions")
	}
	key2 := pbkdf2.Key([]byte("correct horse"), salt2, kdfIterations, 32, sha256.New)
	if _, err := manualDecrypt(t, key2, nonce2, ct2); err != nil {
		t.Fatalf("FAIL: second envelope does not decrypt with its own salt-derived key: %v", err)
	}
}

func TestDecryptRoundTripAndLegacyCompat(t *testing.T) {
	m := NewManager(t.TempDir(), nil, nil)
	src := encryptFixture(t)
	out := filepath.Join(t.TempDir(), "roundtrip.bin")

	// v2 round trip.
	enc := filepath.Join(t.TempDir(), "v2.enc")
	if err := m.Encrypt(src, enc, "pw"); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := m.Decrypt(enc, out, "pw"); err != nil {
		t.Fatalf("decrypt v2: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != "backup payload for round 19" {
		t.Fatalf("FAIL: v2 round trip payload %q err %v", got, err)
	}

	// Wrong password must fail (GCM auth).
	if err := m.Decrypt(enc, out, "wrong"); err == nil {
		t.Fatalf("FAIL: wrong password decrypted the v2 envelope")
	}

	// Legacy v1 envelope (old framing, sha256 key) must still decrypt.
	legacy := filepath.Join(t.TempDir(), "v1.enc")
	legacyKey := sha256.Sum256([]byte("old"))
	block, err := aes.NewCipher(legacyKey[:])
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	legacySalt := bytes.Repeat([]byte{0xAB}, 16)
	legacyNonce := bytes.Repeat([]byte{0xCD}, gcm.NonceSize())
	ct := gcm.Seal(nil, legacyNonce, []byte("legacy payload"), nil)
	var buf bytes.Buffer
	buf.Write(legacySalt)
	buf.Write(legacyNonce)
	buf.Write(ct)
	if err := os.WriteFile(legacy, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	legacyOut := filepath.Join(t.TempDir(), "legacy.out")
	if err := m.Decrypt(legacy, legacyOut, "old"); err != nil {
		t.Fatalf("FAIL: legacy v1 envelope no longer decrypts: %v", err)
	}
	legacyPlain, err := os.ReadFile(legacyOut)
	if err != nil || string(legacyPlain) != "legacy payload" {
		t.Fatalf("FAIL: legacy round trip payload %q err %v", legacyPlain, err)
	}

	// Sanity: the version constant is encoded little-endian-safe (binary
	// package stays imported for future format fields).
	if binary.LittleEndian.Uint16([]byte{0x02, 0x00}) != 2 {
		t.Fatalf("FAIL: version encoding sanity")
	}
}
