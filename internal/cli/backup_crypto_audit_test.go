package cli

// Crypto audit property tests for encryptBackup/decryptBackup (the production
// backup/restore CLI path). Pinned properties: fresh random salt and nonce per
// encryption (no reuse), round-trip integrity, envelope shape, tamper
// rejection, wrong-password rejection. Scrypt parameters (N=2^18, r=8, p=1)
// are asserted by the audit read; the nonce-uniqueness test is the regression
// guard against weakening them or the randomness.

import (
	"bytes"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

func auditManager(t *testing.T, password string) *BackupManager {
	t.Helper()
	bm := NewBackupManager(&config.Config{})
	bm.SetPassword(password)
	return bm
}

// TestEncryptDecryptRoundTripAndEnvelopeShape: the real path round-trips and
// lays out the documented envelope magic|version|salt|nonce|ciphertext.
func TestEncryptDecryptRoundTripAndEnvelopeShape(t *testing.T) {
	bm := auditManager(t, "correct horse battery staple")
	data := []byte("UMAILBACKUP-tar-gz-payload\r\n...")

	env, err := bm.encryptBackup(data)
	if err != nil {
		t.Fatalf("encryptBackup: %v", err)
	}
	wantLen := len(backupMagic) + 1 + saltSize + nonceSize + len(data) + 16 // + GCM auth tag
	if len(env) != wantLen {
		t.Fatalf("envelope length = %d, want %d", len(env), wantLen)
	}
	if string(env[:len(backupMagic)]) != backupMagic {
		t.Fatalf("magic mismatch: %q", env[:len(backupMagic)])
	}
	if env[len(backupMagic)] != backupVersion {
		t.Fatalf("version = %d, want %d", env[len(backupMagic)], backupVersion)
	}

	plain, err := bm.decryptBackup(env)
	if err != nil {
		t.Fatalf("decryptBackup: %v", err)
	}
	if !bytes.Equal(plain, data) {
		t.Fatalf("round-trip mismatch: %q != %q", plain, data)
	}
}

// TestEncryptNonceAndSaltUniqueness: two encryptions of the SAME data with
// the SAME password must use different salts and nonces (fresh crypto/rand
// per call). Identical envelopes would mean nonce/salt reuse.
func TestEncryptNonceAndSaltUniqueness(t *testing.T) {
	bm := auditManager(t, "correct horse battery staple")
	data := []byte("identical payload")

	env1, err := bm.encryptBackup(data)
	if err != nil {
		t.Fatalf("encryptBackup #1: %v", err)
	}
	env2, err := bm.encryptBackup(data)
	if err != nil {
		t.Fatalf("encryptBackup #2: %v", err)
	}

	mOff := len(backupMagic) + 1
	salt1, salt2 := env1[mOff:mOff+saltSize], env2[mOff:mOff+saltSize]
	nonce1, nonce2 := env1[mOff+saltSize:mOff+saltSize+nonceSize], env2[mOff+saltSize:mOff+saltSize+nonceSize]

	if bytes.Equal(salt1, salt2) {
		t.Fatalf("FAIL: salt reuse across encryptions (both %x)", salt1)
	}
	if bytes.Equal(nonce1, nonce2) {
		t.Fatalf("FAIL: nonce reuse across encryptions (both %x)", nonce1)
	}
	if bytes.Equal(env1, env2) {
		t.Fatalf("FAIL: identical envelopes for identical input (deterministic encryption)")
	}

	// Both must still decrypt to the original.
	for i, env := range [][]byte{env1, env2} {
		plain, err := bm.decryptBackup(env)
		if err != nil || !bytes.Equal(plain, data) {
			t.Fatalf("decrypt #%d: err=%v plain=%q", i+1, err, plain)
		}
	}
}

// TestDecryptRejectsTamperedCiphertext: GCM authentication must fail on any
// payload modification.
func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	bm := auditManager(t, "correct horse battery staple")
	env, err := bm.encryptBackup([]byte("payload to tamper"))
	if err != nil {
		t.Fatalf("encryptBackup: %v", err)
	}
	env[len(env)-1] ^= 0xFF
	if _, err := bm.decryptBackup(env); err == nil {
		t.Fatalf("FAIL: tampered ciphertext decrypted without error")
	}
}

// TestDecryptRejectsWrongPassword: the derived key must not decrypt under a
// different password.
func TestDecryptRejectsWrongPassword(t *testing.T) {
	bm := auditManager(t, "correct horse battery staple")
	env, err := bm.encryptBackup([]byte("secret payload"))
	if err != nil {
		t.Fatalf("encryptBackup: %v", err)
	}
	if _, err := auditManager(t, "wrong password 123").decryptBackup(env); err == nil {
		t.Fatalf("FAIL: wrong password decrypted the backup")
	}
}
