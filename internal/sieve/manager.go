package sieve

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// StoredScript holds both source and compiled script
type StoredScript struct {
	Name   string
	Source string
	Script *Script
}

// Manager handles Sieve script storage and execution
type Manager struct {
	scripts       map[string]map[string]*StoredScript // userID -> scriptName -> stored script
	activeScripts map[string]string                   // userID -> activeScriptName
	scriptsMu     sync.RWMutex

	// Vacation cache: prevents spamming the same sender (LRU with max 10000 entries)
	vacationCache    map[string]time.Time
	vacationCacheMu  sync.Mutex
	vacationMaxSize  int
	vacationAccessor []string // LRU tracking
}

// NewManager creates a new Sieve manager
func NewManager() *Manager {
	return &Manager{
		scripts:          make(map[string]map[string]*StoredScript),
		activeScripts:    make(map[string]string),
		vacationCache:    make(map[string]time.Time),
		vacationMaxSize:  10000,
		vacationAccessor: make([]string, 0, 10000),
	}
}

// CompileScript compiles a Sieve script string and returns the Script
func (m *Manager) CompileScript(source string) (*Script, error) {
	p := NewParser(source)
	script, err := p.Parse()
	if err != nil {
		return nil, err
	}
	// F5036: a script requiring an unsupported extension is invalid.
	if err := CheckRequires(script); err != nil {
		return nil, err
	}
	return script, nil
}

// StoreScript stores a script for a user without activating it
func (m *Manager) StoreScript(userID string, scriptName string, source string) error {
	return m.storeScript(userID, scriptName, source, false)
}

// Per-user ManageSieve quota. Scripts live in memory, so without a bound one
// authenticated user could hold an unlimited number of 1 MiB scripts. F5610.
const (
	maxScriptsPerUser       = 100
	maxScriptStoragePerUser = 10 * 1024 * 1024
)

// Quota errors; the server maps them to QUOTA/MAXSCRIPTS and QUOTA (RFC 5804
// §1.3). F5610.
var (
	errQuotaMaxScripts = errors.New("too many scripts")
	errQuotaStorage    = errors.New("script storage quota exceeded")
)

// checkScriptQuotaLocked reports whether storing size octets under scriptName
// (replacing any script of that name) keeps userID within the quota. The
// caller holds scriptsMu. F5610.
func (m *Manager) checkScriptQuotaLocked(userID, scriptName string, size int) error {
	userScripts := m.scripts[userID]
	if _, exists := userScripts[scriptName]; !exists && len(userScripts) >= maxScriptsPerUser {
		return errQuotaMaxScripts
	}
	total := size
	for name, stored := range userScripts {
		if name != scriptName {
			total += len(stored.Source)
		}
	}
	if total > maxScriptStoragePerUser {
		return errQuotaStorage
	}
	return nil
}

// haveSpace is the quota check of HAVESPACE (RFC 5804 §2.5). F5610.
func (m *Manager) haveSpace(userID, scriptName string, size int) error {
	m.scriptsMu.RLock()
	defer m.scriptsMu.RUnlock()
	return m.checkScriptQuotaLocked(userID, scriptName, size)
}

// storeScript compiles and stores a script; with enforceQuota (ManageSieve
// PUTSCRIPT) the quota check and the store happen under one lock. F5610.
func (m *Manager) storeScript(userID, scriptName, source string, enforceQuota bool) error {
	script, err := m.CompileScript(source)
	if err != nil {
		return err
	}

	m.scriptsMu.Lock()
	defer m.scriptsMu.Unlock()

	if enforceQuota {
		if err := m.checkScriptQuotaLocked(userID, scriptName, len(source)); err != nil {
			return err
		}
	}
	if m.scripts[userID] == nil {
		m.scripts[userID] = make(map[string]*StoredScript)
	}
	m.scripts[userID][scriptName] = &StoredScript{
		Name:   scriptName,
		Source: source,
		Script: script,
	}
	return nil
}

// SetActiveScriptByName sets the active script for a user by name
func (m *Manager) SetActiveScriptByName(userID string, scriptName string) error {
	m.scriptsMu.Lock()
	defer m.scriptsMu.Unlock()

	if userScripts, ok := m.scripts[userID]; ok {
		if _, exists := userScripts[scriptName]; !exists {
			return fmt.Errorf("script %q not found", scriptName)
		}
		m.activeScripts[userID] = scriptName
		return nil
	}
	return fmt.Errorf("no scripts found for user")
}

// SetActiveScript sets the active script for a user (stores script with given name and activates)
func (m *Manager) SetActiveScript(userID string, scriptName string, source string) error {
	if err := m.StoreScript(userID, scriptName, source); err != nil {
		return err
	}
	return m.SetActiveScriptByName(userID, scriptName)
}

// GetActiveScript gets the active script for a user
func (m *Manager) GetActiveScript(userID string) (*Script, bool) {
	m.scriptsMu.RLock()
	defer m.scriptsMu.RUnlock()

	activeName, hasActive := m.activeScripts[userID]
	if !hasActive {
		return nil, false
	}

	userScripts, ok := m.scripts[userID]
	if !ok {
		return nil, false
	}

	stored, ok := userScripts[activeName]
	if !ok {
		return nil, false
	}
	return stored.Script, true
}

// HasActiveScript returns true if user has a sieve script
func (m *Manager) HasActiveScript(userID string) bool {
	m.scriptsMu.RLock()
	defer m.scriptsMu.RUnlock()
	_, ok := m.activeScripts[userID]
	return ok
}

// DeleteScript removes a script for a user (by name)
func (m *Manager) DeleteScript(userID string, scriptName string) {
	m.scriptsMu.Lock()
	defer m.scriptsMu.Unlock()

	if userScripts, ok := m.scripts[userID]; ok {
		delete(userScripts, scriptName)
		if m.activeScripts[userID] == scriptName {
			delete(m.activeScripts, userID)
		}
	}
}

// Errors of the ManageSieve-facing script operations; the server maps them
// to RFC 5804 response codes (NONEXISTENT, ACTIVE, ALREADYEXISTS).
var (
	errScriptNotFound = errors.New("script does not exist")
	errScriptActive   = errors.New("script is active")
	errScriptExists   = errors.New("script already exists")
)

// deleteInactiveScript removes a script unless it is the active one
// (RFC 5804 §2.10). Check and delete happen under one lock so a concurrent
// SETACTIVE cannot make the deleted script active. F5463.
func (m *Manager) deleteInactiveScript(userID, scriptName string) error {
	m.scriptsMu.Lock()
	defer m.scriptsMu.Unlock()

	if _, ok := m.scripts[userID][scriptName]; !ok {
		return errScriptNotFound
	}
	if active, ok := m.activeScripts[userID]; ok && active == scriptName {
		return errScriptActive
	}
	delete(m.scripts[userID], scriptName)
	return nil
}

// deactivateScript leaves the user with no active script (SETACTIVE "",
// RFC 5804 §2.8). F5464.
func (m *Manager) deactivateScript(userID string) {
	m.scriptsMu.Lock()
	defer m.scriptsMu.Unlock()
	delete(m.activeScripts, userID)
}

// renameScript renames a script; an active script stays active under its new
// name (RFC 5804 §2.11.1). F5465.
func (m *Manager) renameScript(userID, oldName, newName string) error {
	m.scriptsMu.Lock()
	defer m.scriptsMu.Unlock()

	userScripts := m.scripts[userID]
	stored, ok := userScripts[oldName]
	if !ok {
		return errScriptNotFound
	}
	if _, exists := userScripts[newName]; exists {
		return errScriptExists
	}
	renamed := *stored
	renamed.Name = newName
	userScripts[newName] = &renamed
	delete(userScripts, oldName)
	if active, ok := m.activeScripts[userID]; ok && active == oldName {
		m.activeScripts[userID] = newName
	}
	return nil
}

// ListScripts returns all script names for a user
func (m *Manager) ListScripts(userID string) []string {
	m.scriptsMu.RLock()
	defer m.scriptsMu.RUnlock()

	var result []string
	if userScripts, ok := m.scripts[userID]; ok {
		for name := range userScripts {
			result = append(result, name)
		}
	}
	return result
}

// GetActiveScriptName returns the name of the active script for a user
func (m *Manager) GetActiveScriptName(userID string) string {
	m.scriptsMu.RLock()
	defer m.scriptsMu.RUnlock()
	return m.activeScripts[userID]
}

// GetScript returns a specific script by name for a user
func (m *Manager) GetScript(userID string, scriptName string) (*Script, bool) {
	m.scriptsMu.RLock()
	defer m.scriptsMu.RUnlock()

	if userScripts, ok := m.scripts[userID]; ok {
		stored, exists := userScripts[scriptName]
		if !exists {
			return nil, false
		}
		return stored.Script, true
	}
	return nil, false
}

// GetScriptSource returns the source of a specific script by name for a user
func (m *Manager) GetScriptSource(userID string, scriptName string) string {
	m.scriptsMu.RLock()
	defer m.scriptsMu.RUnlock()

	if userScripts, ok := m.scripts[userID]; ok {
		if stored, exists := userScripts[scriptName]; exists {
			return stored.Source
		}
	}
	return ""
}

// scriptSource is GetScriptSource with an existence flag, so an empty
// script is distinguishable from a missing one (GETSCRIPT). F5467.
func (m *Manager) scriptSource(userID, scriptName string) (string, bool) {
	m.scriptsMu.RLock()
	defer m.scriptsMu.RUnlock()
	stored, ok := m.scripts[userID][scriptName]
	if !ok {
		return "", false
	}
	return stored.Source, true
}

// ProcessMessage runs the Sieve script for a user and returns actions
func (m *Manager) ProcessMessage(userID string, msg *MessageContext) ([]Action, error) {
	script, ok := m.GetActiveScript(userID)
	if !ok {
		// No script, default keep
		return []Action{KeepAction{}}, nil
	}

	interp := NewInterpreter(script)
	actions, err := interp.Execute(msg)
	if err != nil {
		return nil, err
	}

	return actions, nil
}

// ShouldSendVacation checks if we should send a vacation reply to this sender
// Returns false if we sent one recently (within the minimum interval)
func (m *Manager) ShouldSendVacation(sender string, days int) bool {
	m.vacationCacheMu.Lock()
	defer m.vacationCacheMu.Unlock()

	lastSent, ok := m.vacationCache[sender]
	if !ok {
		return true
	}

	// Minimum interval is 1 day regardless of user's preference
	interval := time.Duration(days) * 24 * time.Hour
	if interval < 24*time.Hour {
		interval = 24 * time.Hour
	}

	return time.Since(lastSent) >= interval
}

// RecordVacationSent records that we sent a vacation reply to this sender
func (m *Manager) RecordVacationSent(sender string) {
	m.vacationCacheMu.Lock()
	defer m.vacationCacheMu.Unlock()

	// LRU eviction: remove oldest 25% if at capacity
	if len(m.vacationCache) >= m.vacationMaxSize {
		removeCount := m.vacationMaxSize / 4
		for i := 0; i < removeCount && len(m.vacationAccessor) > 0; i++ {
			oldest := m.vacationAccessor[0]
			m.vacationAccessor = m.vacationAccessor[1:]
			delete(m.vacationCache, oldest)
		}
	}

	m.vacationCache[sender] = time.Now()
	m.vacationAccessor = append(m.vacationAccessor, sender)
}

// CheckAndRecordVacation atomically checks if we should send a vacation reply and records that we will.
// This prevents race conditions where multiple goroutines could send vacation replies for the same sender.
// Days-granular with a 24h floor (RFC 5230 :days semantics); use
// CheckAndRecordVacationFor for RFC 6131 :seconds windows.
func (m *Manager) CheckAndRecordVacation(sender string, days int) bool {
	interval := time.Duration(days) * 24 * time.Hour
	// Minimum interval is 1 day regardless of user's preference
	if interval < 24*time.Hour {
		interval = 24 * time.Hour
	}
	return m.CheckAndRecordVacationFor(sender, interval)
}

// CheckAndRecordVacationFor is the duration-aware form of CheckAndRecordVacation
// (RFC 6131 :seconds): interval is honored exactly, and a window of 0 or less
// replies to every delivery (":seconds 0" disables suppression).
func (m *Manager) CheckAndRecordVacationFor(sender string, interval time.Duration) bool {
	m.vacationCacheMu.Lock()
	defer m.vacationCacheMu.Unlock()

	if interval > 0 {
		lastSent, ok := m.vacationCache[sender]
		if ok && time.Since(lastSent) < interval {
			return false
		}
	}

	// LRU eviction: remove oldest 25% if at capacity
	if len(m.vacationCache) >= m.vacationMaxSize {
		removeCount := m.vacationMaxSize / 4
		for i := 0; i < removeCount && len(m.vacationAccessor) > 0; i++ {
			oldest := m.vacationAccessor[0]
			m.vacationAccessor = m.vacationAccessor[1:]
			delete(m.vacationCache, oldest)
		}
	}

	m.vacationCache[sender] = time.Now()
	m.vacationAccessor = append(m.vacationAccessor, sender)
	return true
}

// GetVacationInterval returns the minimum interval for vacation replies
func (m *Manager) GetVacationInterval(days int) time.Duration {
	interval := time.Duration(days) * 24 * time.Hour
	if interval < 24*time.Hour {
		interval = 24 * time.Hour
	}
	return interval
}

// ValidateScript validates a Sieve script without executing it
func (m *Manager) ValidateScript(source string) error {
	_, err := m.CompileScript(source)
	return err
}
