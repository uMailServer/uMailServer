package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// encryptFile encrypts plaintext into dir/enc.bin using the real Manager.
func encryptFile(t *testing.T, dir string, plaintext []byte, password string) string {
	t.Helper()

	src := filepath.Join(dir, "src.bin")
	enc := filepath.Join(dir, "enc.bin")
	if err := os.WriteFile(src, plaintext, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := NewManager(dir, nil, nil).Encrypt(src, enc, password); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return enc
}

// TestEncryptDecryptRoundTrip asserts the contract of the pair: whatever
// Encrypt writes, Decrypt must be able to restore with the same password.
// The two must agree on the nonce length; if they disagree the ciphertext is
// read at the wrong offset and every encrypted backup becomes unrecoverable.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
		password  string
	}{
		{name: "typical message", plaintext: []byte("Subject: hi\r\n\r\nbody"), password: "hunter2"},
		{name: "empty payload", plaintext: nil, password: "pw"},
		{name: "single byte", plaintext: []byte("x"), password: ""},
		{name: "exactly one GCM block", plaintext: bytes.Repeat([]byte("A"), 16), password: "pw"},
		{name: "binary payload", plaintext: []byte{0x00, 0x1f, 0x8b, 0xff, 0x00}, password: "p@ss w0rd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			enc := encryptFile(t, dir, tt.plaintext, tt.password)
			out := filepath.Join(dir, "out.bin")

			if err := NewManager(dir, nil, nil).Decrypt(enc, out, tt.password); err != nil {
				t.Fatalf("Decrypt of a file produced by Encrypt with the same password: %v", err)
			}

			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("read restored file: %v", err)
			}
			if !bytes.Equal(got, tt.plaintext) {
				t.Fatalf("restored data differs\n got: %q\nwant: %q", got, tt.plaintext)
			}
		})
	}
}

// TestDecryptRejectsWrongPassword covers the secondary branch the fix now
// reaches: an undecryptable file must return an error, not panic, and must
// not write a restored file.
func TestDecryptRejectsWrongPassword(t *testing.T) {
	dir := t.TempDir()
	enc := encryptFile(t, dir, []byte("top secret"), "right-password")
	out := filepath.Join(dir, "out.bin")

	if err := NewManager(dir, nil, nil).Decrypt(enc, out, "wrong-password"); err == nil {
		t.Fatal("Decrypt accepted the wrong password")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("Decrypt wrote an output file despite failing: stat err = %v", err)
	}
}

// TestEncryptOnDiskLayout pins the wire layout Encrypt produces, so the two
// halves of the pair cannot silently drift apart again:
// salt(16) + nonce(gcm.NonceSize()) + ciphertext(len + GCM tag).
func TestEncryptOnDiskLayout(t *testing.T) {
	dir := t.TempDir()
	plaintext := []byte("layout probe")

	info, err := os.Stat(encryptFile(t, dir, plaintext, "pw"))
	if err != nil {
		t.Fatalf("stat encrypted file: %v", err)
	}

	// AES-256-GCM: 12-byte nonce, 16-byte tag.
	const want = 16 /*salt*/ + 12 /*nonce*/ + 16 /*tag*/
	if got := int(info.Size()); got != len(plaintext)+want {
		t.Fatalf("encrypted size = %d, want %d (salt 16 + nonce 12 + ciphertext %d)",
			got, len(plaintext)+want, len(plaintext))
	}
}
