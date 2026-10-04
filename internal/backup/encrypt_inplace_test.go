package backup

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptPreservesSourcePayload(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base, nil, nil)
	payload := []byte("Subject: archived message\r\n\r\nimportant mail\r\n")
	src := filepath.Join(base, "original")
	enc := filepath.Join(base, "encrypted")
	out := filepath.Join(base, "out")
	if err := os.WriteFile(src, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Encrypt(src, enc, "fixture-password"); err != nil {
		t.Fatal("CONTROL FAILED", err)
	}
	if err := m.Decrypt(enc, out, "fixture-password"); err != nil {
		t.Fatal("CONTROL FAILED", err)
	}
	data, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatal("CONTROL FAILED", err)
	}
	fmt.Println("CONTROL EXPECTED: payload round trip | ACTUAL: payload round trip")
	if err := m.Encrypt(src, src, "fixture-password"); err != nil {
		t.Fatal("unexpected same-path encryption error", err)
	}
	if err := m.Decrypt(src, out, "fixture-password"); err != nil {
		t.Fatal("unexpected decryption error", err)
	}
	data, err = os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("EXPECTED: %q | ACTUAL: %q\n", payload, data)
	if !bytes.Equal(data, payload) {
		fmt.Println("PROBLEM CONFIRMED")
		t.FailNow()
	}
	fmt.Println("PROBLEM NOT REPRODUCED")
	empty := filepath.Join(base, "empty")
	if err := os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Encrypt(empty, empty, "fixture-password"); err != nil {
		t.Fatal(err)
	}
	if err := m.Decrypt(empty, out, "fixture-password"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil || len(got) != 0 {
		t.Fatal("empty in-place round trip", err)
	}
	if err := os.WriteFile(enc, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Encrypt(filepath.Join(base, "missing"), enc, "fixture-password"); err == nil {
		t.Fatal("missing source accepted")
	}
	got, err = os.ReadFile(enc)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("failed source read modified destination", err)
	}
	fmt.Println("FIX VERIFIED")
}
