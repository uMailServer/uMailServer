package push

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewServiceWithConfig_OverrideKeys(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewServiceWithConfig(dir, Config{
		VAPIDPublicKey:  "BPub-OVERRIDE",
		VAPIDPrivateKey: "Priv-OVERRIDE",
		Subject:         "https://example.com/operator",
	}, nil)
	if err != nil {
		t.Fatalf("NewServiceWithConfig: %v", err)
	}
	if svc.config.VAPIDPublicKey != "BPub-OVERRIDE" {
		t.Errorf("public key = %q", svc.config.VAPIDPublicKey)
	}
	if svc.config.VAPIDPrivateKey != "Priv-OVERRIDE" {
		t.Errorf("private key = %q", svc.config.VAPIDPrivateKey)
	}
	if svc.config.Subject != "https://example.com/operator" {
		t.Errorf("subject = %q", svc.config.Subject)
	}
	// vapid.json should NOT have been written when overrides are supplied.
	if _, err := os.Stat(filepath.Join(dir, "vapid.json")); !os.IsNotExist(err) {
		t.Error("vapid.json must not exist when keys come from override")
	}
}

func TestNewServiceWithConfig_NoKeys_LoadsOrGenerates(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewServiceWithConfig(dir, Config{Subject: "mailto:a@b.com"}, nil)
	if err != nil {
		t.Fatalf("NewServiceWithConfig: %v", err)
	}
	if svc.config.VAPIDPublicKey == "" || svc.config.VAPIDPrivateKey == "" {
		t.Error("expected auto-generated keys")
	}
	if svc.config.Subject != "mailto:a@b.com" {
		t.Errorf("subject override not applied: %q", svc.config.Subject)
	}
	// vapid.json SHOULD exist now (generated).
	if _, err := os.Stat(filepath.Join(dir, "vapid.json")); err != nil {
		t.Errorf("vapid.json missing after generation: %v", err)
	}
}

func TestNewServiceWithConfig_OverrideKeys_DoesNotOverwriteOnDiskFile(t *testing.T) {
	dir := t.TempDir()
	// Pre-seed an on-disk file with different keys so we can confirm it
	// stays untouched when overrides are used.
	disk := Config{VAPIDPublicKey: "DISK-PUB", VAPIDPrivateKey: "DISK-PRIV", Subject: "mailto:disk@example.com"}
	data, _ := json.Marshal(disk)
	if err := os.WriteFile(filepath.Join(dir, "vapid.json"), data, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc, err := NewServiceWithConfig(dir, Config{
		VAPIDPublicKey:  "OVERRIDE-PUB",
		VAPIDPrivateKey: "OVERRIDE-PRIV",
	}, nil)
	if err != nil {
		t.Fatalf("NewServiceWithConfig: %v", err)
	}
	if svc.config.VAPIDPublicKey != "OVERRIDE-PUB" {
		t.Errorf("override not applied: %q", svc.config.VAPIDPublicKey)
	}
	// Re-read disk file and confirm it's untouched.
	got, _ := os.ReadFile(filepath.Join(dir, "vapid.json"))
	var onDisk Config
	if err := json.Unmarshal(got, &onDisk); err != nil {
		t.Fatalf("disk unmarshal: %v", err)
	}
	if onDisk.VAPIDPublicKey != "DISK-PUB" {
		t.Errorf("on-disk file was overwritten: %+v", onDisk)
	}
}

func TestNewServiceWithConfig_PartialOverride_FallsBackToDisk(t *testing.T) {
	// Per validation, partial keys are rejected at the config layer. But the
	// constructor itself should treat half-set as "no override" (defensive).
	dir := t.TempDir()
	svc, err := NewServiceWithConfig(dir, Config{VAPIDPublicKey: "only-pub"}, nil)
	if err != nil {
		t.Fatalf("NewServiceWithConfig: %v", err)
	}
	// Auto-generated keys, NOT the user-supplied half.
	if svc.config.VAPIDPublicKey == "only-pub" {
		t.Error("partial override should not be applied")
	}
}

func TestNewService_BackwardsCompat(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewService(dir, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if svc.config.VAPIDPublicKey == "" {
		t.Error("expected auto-generated public key")
	}
	if svc.config.Subject != "mailto:admin@umailserver.local" {
		t.Errorf("default subject changed: %q", svc.config.Subject)
	}
}
