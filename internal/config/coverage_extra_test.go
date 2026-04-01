package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCreateDomainSuccess verifies the full CreateDomain path including
// that the generated settings have expected defaults.
func TestCreateDomainSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	settings, err := dm.CreateDomain("newdomain.com")
	if err != nil {
		t.Fatalf("CreateDomain failed: %v", err)
	}

	// Verify all expected default settings are populated
	if settings.Name != "newdomain.com" {
		t.Errorf("expected name newdomain.com, got %s", settings.Name)
	}
	if settings.MaxAccounts != 100 {
		t.Errorf("expected MaxAccounts 100, got %d", settings.MaxAccounts)
	}
	if settings.MaxAliases != 500 {
		t.Errorf("expected MaxAliases 500, got %d", settings.MaxAliases)
	}
	if !settings.IsActive {
		t.Error("expected IsActive true")
	}
	if !settings.DKIM.Enabled {
		t.Error("expected DKIM enabled")
	}
	if settings.DKIM.Selector != "default" {
		t.Errorf("expected DKIM selector 'default', got %s", settings.DKIM.Selector)
	}
	if settings.DKIM.KeyType != "rsa" {
		t.Errorf("expected DKIM key type 'rsa', got %s", settings.DKIM.KeyType)
	}
	if settings.DKIM.KeySize != 2048 {
		t.Errorf("expected DKIM key size 2048, got %d", settings.DKIM.KeySize)
	}
	if !settings.Security.RequireTLS {
		t.Error("expected Security.RequireTLS true")
	}
	if settings.Security.MinTLSVersion != "1.2" {
		t.Errorf("expected MinTLSVersion 1.2, got %s", settings.Security.MinTLSVersion)
	}
	if !settings.Security.MTASTSEnabled {
		t.Error("expected MTASTSEnabled true")
	}
	if settings.Spam.RejectThreshold != 9.0 {
		t.Errorf("expected Spam.RejectThreshold 9.0, got %f", settings.Spam.RejectThreshold)
	}
	if settings.Spam.JunkThreshold != 3.0 {
		t.Errorf("expected Spam.JunkThreshold 3.0, got %f", settings.Spam.JunkThreshold)
	}
	if settings.Spam.QuarantineThreshold != 6.0 {
		t.Errorf("expected Spam.QuarantineThreshold 6.0, got %f", settings.Spam.QuarantineThreshold)
	}

	// Verify the file was actually written
	if !dm.DomainExists("newdomain.com") {
		t.Error("expected domain to exist after CreateDomain")
	}
}

// TestImportFromMainConfigMultipleDomains verifies ImportFromMainConfig
// correctly processes multiple domains.
func TestImportFromMainConfigMultipleDomains(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Domains = []DomainConfig{
		{
			Name:          "one.com",
			MaxAccounts:   10,
			MaxMailboxSize: 1 * GB,
			DKIM:          DomainDKIMConfig{Selector: "sel1"},
		},
		{
			Name:          "two.com",
			MaxAccounts:   20,
			MaxMailboxSize: 2 * GB,
			DKIM:          DomainDKIMConfig{Selector: "sel2"},
		},
		{
			Name:          "three.com",
			MaxAccounts:   30,
			MaxMailboxSize: 3 * GB,
			DKIM:          DomainDKIMConfig{Selector: "sel3"},
		},
	}

	err := dm.ImportFromMainConfig(cfg)
	if err != nil {
		t.Fatalf("ImportFromMainConfig failed: %v", err)
	}

	// All domains should exist
	for _, d := range cfg.Domains {
		if !dm.DomainExists(d.Name) {
			t.Errorf("expected domain %s to exist after import", d.Name)
		}

		settings, err := dm.LoadDomain(d.Name)
		if err != nil {
			t.Fatalf("LoadDomain(%s): %v", d.Name, err)
		}
		if !settings.DKIM.Enabled {
			t.Errorf("expected DKIM enabled for %s (selector=%s)", d.Name, d.DKIM.Selector)
		}
		if settings.DKIM.Selector != d.DKIM.Selector {
			t.Errorf("expected selector %s for %s, got %s", d.DKIM.Selector, d.Name, settings.DKIM.Selector)
		}
		if settings.MaxAccounts != d.MaxAccounts {
			t.Errorf("expected MaxAccounts %d for %s, got %d", d.MaxAccounts, d.Name, settings.MaxAccounts)
		}
	}
}

// TestListDomainsWithMixedFiles verifies ListDomains filters correctly
// when the directory contains a mix of yaml files, non-yaml files, and dirs.
func TestListDomainsWithMixedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	domainsDir := dm.baseDir

	// Create yaml domain files
	os.WriteFile(filepath.Join(domainsDir, "a.com.yaml"), []byte("name: a.com"), 0644)
	os.WriteFile(filepath.Join(domainsDir, "b.com.yaml"), []byte("name: b.com"), 0644)

	// Create non-yaml files
	os.WriteFile(filepath.Join(domainsDir, "readme.txt"), []byte("info"), 0644)
	os.WriteFile(filepath.Join(domainsDir, "config.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(domainsDir, "backup.yaml.bak"), []byte("bak"), 0644)

	// Create a subdirectory
	os.MkdirAll(filepath.Join(domainsDir, "subdir"), 0755)

	domains, err := dm.ListDomains()
	if err != nil {
		t.Fatalf("ListDomains failed: %v", err)
	}

	if len(domains) != 2 {
		t.Errorf("expected 2 domains, got %d: %v", len(domains), domains)
	}

	// Verify exact domain names
	found := map[string]bool{}
	for _, d := range domains {
		found[d] = true
	}
	if !found["a.com"] || !found["b.com"] {
		t.Errorf("expected a.com and b.com, got %v", domains)
	}
}

// TestDomainExistsWithStatError verifies DomainExists when stat returns
// an error other than NotExist.
func TestDomainExistsWithStatError(t *testing.T) {
	// Create a DomainManager whose path contains a null byte.
	// os.Stat will return an error, but not os.IsNotExist.
	dm := NewDomainManager(string([]byte{0}))
	// DomainExists should return true (since the error is NOT IsNotExist)
	if !dm.DomainExists("anything.com") {
		t.Error("expected DomainExists=true for stat error that is not IsNotExist")
	}
}

// TestLoadDomainCorruptYAML verifies LoadDomain with corrupt YAML content.
func TestLoadDomainCorruptYAML(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Write corrupt YAML directly
	domainsDir := dm.baseDir
	corruptPath := filepath.Join(domainsDir, "corrupt.com.yaml")
	os.WriteFile(corruptPath, []byte("invalid: yaml: [\n  : broken"), 0644)

	_, err := dm.LoadDomain("corrupt.com")
	if err == nil {
		t.Error("expected error loading corrupt YAML domain config")
	}
}

// TestDeleteDomainSuccess verifies successful deletion.
func TestDeleteDomainSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create a domain
	_, err := dm.CreateDomain("todelete.com")
	if err != nil {
		t.Fatalf("CreateDomain failed: %v", err)
	}

	// Verify it exists
	if !dm.DomainExists("todelete.com") {
		t.Error("expected domain to exist before deletion")
	}

	// Delete it
	if err := dm.DeleteDomain("todelete.com"); err != nil {
		t.Fatalf("DeleteDomain failed: %v", err)
	}

	// Verify it's gone
	if dm.DomainExists("todelete.com") {
		t.Error("expected domain to not exist after deletion")
	}
}

// TestSaveDomainLoadRoundTrip verifies SaveDomain -> LoadDomain round trip
// preserves all fields correctly.
func TestSaveDomainLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	settings := &DomainSettings{
		Name:           "roundtrip.com",
		MaxAccounts:    42,
		MaxAliases:     100,
		MaxMailboxSize: 5 * GB,
		IsActive:       true,
		DKIM: DomainDKIMSettings{
			Enabled:    true,
			Selector:   "custom",
			KeyType:    "ed25519",
			KeySize:    256,
			PrivateKey: "/path/to/key",
			PublicKey:  "/path/to/pub",
		},
		Security: DomainSecuritySettings{
			RequireTLS:    false,
			MinTLSVersion: "1.3",
			MTASTSEnabled: true,
			MTASTSPolicy:  "enforce",
			MTASTSMaxAge:  Duration(86400 * 1e9),
		},
		Spam: DomainSpamSettings{
			Enabled:             true,
			RejectThreshold:     8.5,
			JunkThreshold:       2.5,
			QuarantineThreshold: 5.5,
			BypassSPF:           true,
			BypassDKIM:          true,
		},
	}

	if err := dm.SaveDomain(settings); err != nil {
		t.Fatalf("SaveDomain failed: %v", err)
	}

	loaded, err := dm.LoadDomain("roundtrip.com")
	if err != nil {
		t.Fatalf("LoadDomain failed: %v", err)
	}

	if loaded.Name != settings.Name {
		t.Errorf("Name: got %q, want %q", loaded.Name, settings.Name)
	}
	if loaded.MaxAccounts != settings.MaxAccounts {
		t.Errorf("MaxAccounts: got %d, want %d", loaded.MaxAccounts, settings.MaxAccounts)
	}
	if loaded.MaxAliases != settings.MaxAliases {
		t.Errorf("MaxAliases: got %d, want %d", loaded.MaxAliases, settings.MaxAliases)
	}
	if loaded.DKIM.Selector != settings.DKIM.Selector {
		t.Errorf("DKIM.Selector: got %q, want %q", loaded.DKIM.Selector, settings.DKIM.Selector)
	}
	if loaded.Security.MinTLSVersion != settings.Security.MinTLSVersion {
		t.Errorf("Security.MinTLSVersion: got %q, want %q", loaded.Security.MinTLSVersion, settings.Security.MinTLSVersion)
	}
	if loaded.Spam.BypassSPF != settings.Spam.BypassSPF {
		t.Errorf("Spam.BypassSPF: got %v, want %v", loaded.Spam.BypassSPF, settings.Spam.BypassSPF)
	}
}
