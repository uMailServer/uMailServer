package backup

// Verification for the legacy v1 decrypt design decision (2026-10-02): the v1
// read path is KEPT (old backups stay recoverable) and marked DEPRECATED —
// every v1 decryption logs a warning with migration guidance; v2 decryptions
// stay silent.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecryptV1LogsDeprecationWarningV2StaysSilent(t *testing.T) {
	m := NewManager(t.TempDir(), nil, nil)
	password := "old"
	out := filepath.Join(t.TempDir(), "out")

	// Hand-built legacy v1 envelope: salt(16) | nonce(12) | ciphertext,
	// keyed by the unsalted SHA-256(password) — the pre-v2 framing.
	legacy := filepath.Join(t.TempDir(), "v1.enc")
	legacyKey := sha256.Sum256([]byte(password))
	block, err := aes.NewCipher(legacyKey[:])
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("NewGCM: %v", err)
	}
	nonce := bytes.Repeat([]byte{0xCD}, gcm.NonceSize())
	ct := gcm.Seal(nil, nonce, []byte("legacy payload"), nil)
	if err := os.WriteFile(legacy, append(bytes.Repeat([]byte{0xAB}, 16), append(nonce, ct...)...), 0o600); err != nil {
		t.Fatalf("write v1 envelope: %v", err)
	}

	// Capture the default slog output.
	orig := slog.Default()
	buf := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

	if err := m.Decrypt(legacy, out, password); err != nil {
		t.Fatalf("Decrypt(v1): %v", err)
	}
	if !strings.Contains(buf.String(), "deprecated legacy v1 backup envelope") {
		t.Fatalf("FAIL: the v1 deprecation warning did not fire (log: %q)", buf.String())
	}
	if !strings.Contains(buf.String(), "src=") {
		t.Fatalf("FAIL: the warning lacks the source path attribute (log: %q)", buf.String())
	}

	// A v2 envelope (the real Encrypt path) must stay silent.
	v2src := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(v2src, []byte("v2 payload"), 0o600); err != nil {
		t.Fatalf("write v2 source: %v", err)
	}
	v2enc := filepath.Join(t.TempDir(), "v2.enc")
	if err := m.Encrypt(v2src, v2enc, password); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	buf.Reset()
	if err := m.Decrypt(v2enc, out, password); err != nil {
		t.Fatalf("Decrypt(v2): %v", err)
	}
	if strings.Contains(buf.String(), "deprecated legacy v1 backup envelope") {
		t.Fatalf("FAIL: the deprecation warning fired on a v2 envelope (log: %q)", buf.String())
	}
}
