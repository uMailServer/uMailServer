package config

import (
	"log/slog"
	"os"
	"sync"
	"time"
)

// ChangeHandler is called when the config changes
type ChangeHandler func(oldCfg, newCfg *Config)

// Watcher watches a config file for changes
type Watcher struct {
	path        string
	logger      *slog.Logger
	onChange    ChangeHandler
	stopCh      chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
	lastModTime time.Time
	lastHash    string
	currentCfg  *Config
	mutex       sync.RWMutex
}

// NewWatcher creates a new config watcher
func NewWatcher(path string, logger *slog.Logger, onChange ChangeHandler) *Watcher {
	if logger == nil {
		logger = slog.Default()
	}

	return &Watcher{
		path:     path,
		logger:   logger,
		onChange: onChange,
		stopCh:   make(chan struct{}),
	}
}

// Start begins watching the config file
func (w *Watcher) Start(interval time.Duration) error {
	// Get initial file info
	info, err := os.Stat(w.path)
	if err != nil {
		return err
	}

	w.lastModTime = info.ModTime()

	// Calculate initial hash
	hash, err := w.fileHash()
	if err != nil {
		return err
	}
	w.lastHash = hash

	w.wg.Add(1)
	go w.watch(interval)

	w.logger.Info("Config watcher started", "path", w.path, "interval", interval)
	return nil
}

// Stop stops the watcher
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
	w.wg.Wait()
	w.logger.Info("Config watcher stopped")
}

// watch polls the file for changes
func (w *Watcher) watch(interval time.Duration) {
	defer w.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if w.check() {
				w.reload()
			}
		case <-w.stopCh:
			return
		}
	}
}

// check returns true if the file has changed
func (w *Watcher) check() bool {
	info, err := os.Stat(w.path)
	if err != nil {
		w.logger.Error("Failed to stat config file", "error", err)
		return false
	}

	// Check modification time first
	if info.ModTime().Equal(w.lastModTime) {
		return false
	}

	// Verify with hash
	hash, err := w.fileHash()
	if err != nil {
		w.logger.Error("Failed to hash config file", "error", err)
		return false
	}

	if hash == w.lastHash {
		// File was touched but content didn't change
		w.lastModTime = info.ModTime()
		return false
	}

	return true
}

// reload loads the new config and calls the handler
func (w *Watcher) reload() {
	w.logger.Info("Config file changed, reloading...")

	// Load new config
	newCfg, err := Load(w.path)
	if err != nil {
		w.logger.Error("Failed to reload config", "error", err)
		return
	}

	w.mutex.Lock()
	oldCfg := w.currentCfg
	w.currentCfg = newCfg

	// Update tracking info
	info, _ := os.Stat(w.path)
	if info != nil {
		w.lastModTime = info.ModTime()
	}
	w.lastHash, _ = w.fileHash()
	w.mutex.Unlock()

	// Call handler
	if w.onChange != nil {
		w.onChange(oldCfg, newCfg)
	}

	w.logger.Info("Config reloaded successfully")
}

// fileHash calculates a simple hash of the file content
func (w *Watcher) fileHash() (string, error) {
	data, err := os.ReadFile(w.path)
	if err != nil {
		return "", err
	}

	// Simple hash: sum of bytes
	var sum uint64
	for _, b := range data {
		sum += uint64(b)
	}

	return string(rune(sum)), nil
}

// SetCurrentConfig sets the current config (called after initial load)
func (w *Watcher) SetCurrentConfig(cfg *Config) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.currentCfg = cfg
}

// GetCurrentConfig returns the current config
func (w *Watcher) GetCurrentConfig() *Config {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.currentCfg
}

// ReloadableConfig holds a reloadable configuration
type ReloadableConfig struct {
	cfg     *Config
	watcher *Watcher
	mutex   sync.RWMutex
}

// NewReloadable creates a new reloadable config
func NewReloadable(path string, logger *slog.Logger) (*ReloadableConfig, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	rc := &ReloadableConfig{
		cfg: cfg,
	}

	watcher := NewWatcher(path, logger, func(oldCfg, newCfg *Config) {
		rc.mutex.Lock()
		rc.cfg = newCfg
		rc.mutex.Unlock()
	})
	watcher.SetCurrentConfig(cfg)
	rc.watcher = watcher

	return rc, nil
}

// Start begins watching for changes
func (rc *ReloadableConfig) Start(interval time.Duration) error {
	return rc.watcher.Start(interval)
}

// Stop stops watching
func (rc *ReloadableConfig) Stop() {
	rc.watcher.Stop()
}

// Get returns the current config
func (rc *ReloadableConfig) Get() *Config {
	rc.mutex.RLock()
	defer rc.mutex.RUnlock()
	return rc.cfg
}
