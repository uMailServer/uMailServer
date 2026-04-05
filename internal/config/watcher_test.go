package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewWatcher(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	// Create initial config file
	content := `
server:
  hostname: test.example.com
  data_dir: /tmp/test
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}

	watcher := NewWatcher(configPath, nil, func(oldCfg, newCfg *Config) {})
	if watcher == nil {
		t.Fatal("NewWatcher returned nil")
	}

	if watcher.path != configPath {
		t.Errorf("expected path %s, got %s", configPath, watcher.path)
	}
}

func TestWatcher_StartStop(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	// Create initial config file
	content := `
server:
  hostname: test.example.com
  data_dir: /tmp/test
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}

	watcher := NewWatcher(configPath, nil, nil)

	err := watcher.Start(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	watcher.Stop()
}

func TestWatcher_ConfigChange(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	// Create initial config file
	content1 := `
server:
  hostname: test1.example.com
  data_dir: /tmp/test1
security:
  jwt_secret: "this-is-a-32-character-secret-min"
`
	if err := os.WriteFile(configPath, []byte(content1), 0644); err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}

	changeCalled := false
	var receivedNewCfg *Config

	watcher := NewWatcher(configPath, nil, func(oldCfg, newCfg *Config) {
		changeCalled = true
		_ = oldCfg
		receivedNewCfg = newCfg
	})

	err := watcher.Start(50 * time.Millisecond)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer watcher.Stop()

	// Wait for initial load
	time.Sleep(100 * time.Millisecond)

	// Modify the file
	content2 := `
server:
  hostname: test2.example.com
  data_dir: /tmp/test2
security:
  jwt_secret: "this-is-a-32-character-secret-min"
`
	if err := os.WriteFile(configPath, []byte(content2), 0644); err != nil {
		t.Fatalf("failed to modify test config: %v", err)
	}

	// Wait for the change to be detected
	time.Sleep(200 * time.Millisecond)

	if !changeCalled {
		t.Error("change handler should have been called")
	}

	if receivedNewCfg != nil && receivedNewCfg.Server.Hostname != "test2.example.com" {
		t.Errorf("expected new hostname 'test2.example.com', got %s", receivedNewCfg.Server.Hostname)
	}
}

func TestWatcher_SetGetCurrentConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	// Create minimal config file
	content := `
server:
  hostname: test.example.com
  data_dir: /tmp/test
security:
  jwt_secret: "this-is-a-32-character-secret-min"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	watcher := NewWatcher(configPath, nil, nil)
	watcher.SetCurrentConfig(cfg)

	retrieved := watcher.GetCurrentConfig()
	if retrieved != cfg {
		t.Error("GetCurrentConfig should return the same config")
	}
}

func TestNewReloadable(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	// Create config file
	content := `
server:
  hostname: test.example.com
  data_dir: /tmp/test
security:
  jwt_secret: "this-is-a-32-character-secret-min"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}

	rc, err := NewReloadable(configPath, nil)
	if err != nil {
		t.Fatalf("NewReloadable failed: %v", err)
	}

	if rc == nil {
		t.Fatal("NewReloadable returned nil")
	}

	cfg := rc.Get()
	if cfg == nil {
		t.Fatal("Get returned nil config")
	}

	if cfg.Server.Hostname != "test.example.com" {
		t.Errorf("expected hostname 'test.example.com', got %s", cfg.Server.Hostname)
	}
}

func TestNewReloadable_InvalidPath(t *testing.T) {
	// Create a file path that exists but has invalid content
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	// Write invalid YAML that will cause parse errors
	if err := os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0644); err != nil {
		t.Fatalf("failed to create invalid config: %v", err)
	}

	_, err := NewReloadable(configPath, nil)
	if err == nil {
		t.Error("should return error for invalid config")
	}
}

func TestReloadableConfig_StartStop(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	// Create config file
	content := `
server:
  hostname: test.example.com
  data_dir: /tmp/test
security:
  jwt_secret: "this-is-a-32-character-secret-min"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}

	rc, err := NewReloadable(configPath, nil)
	if err != nil {
		t.Fatalf("NewReloadable failed: %v", err)
	}

	err = rc.Start(50 * time.Millisecond)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	rc.Stop()
}

func TestWatcher_fileHash(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	content := "test content"
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	watcher := NewWatcher(configPath, nil, nil)

	hash1, err := watcher.fileHash()
	if err != nil {
		t.Fatalf("fileHash failed: %v", err)
	}

	// Same content should produce same hash
	hash2, err := watcher.fileHash()
	if err != nil {
		t.Fatalf("fileHash failed: %v", err)
	}

	if hash1 != hash2 {
		t.Error("same content should produce same hash")
	}

	// Different content should produce different hash
	if err := os.WriteFile(configPath, []byte("different content"), 0644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	hash3, err := watcher.fileHash()
	if err != nil {
		t.Fatalf("fileHash failed: %v", err)
	}

	if hash1 == hash3 {
		t.Error("different content should produce different hash")
	}
}

func TestWatcher_check(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	// Create initial file
	content := "initial content"
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	watcher := NewWatcher(configPath, nil, nil)
	watcher.Start(50 * time.Millisecond)
	defer watcher.Stop()

	// Wait for initial check
	time.Sleep(100 * time.Millisecond)

	// File hasn't changed
	if watcher.check() {
		t.Error("check should return false when file hasn't changed")
	}

	// Modify file
	time.Sleep(10 * time.Millisecond) // Ensure different mod time
	if err := os.WriteFile(configPath, []byte("changed content"), 0644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	// Wait for change to be detectable
	time.Sleep(100 * time.Millisecond)

	// File has changed
	if !watcher.check() {
		t.Error("check should return true when file has changed")
	}
}
