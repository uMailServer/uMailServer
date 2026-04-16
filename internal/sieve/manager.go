package sieve

import (
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
	return p.Parse()
}

// StoreScript stores a script for a user without activating it
func (m *Manager) StoreScript(userID string, scriptName string, source string) error {
	script, err := m.CompileScript(source)
	if err != nil {
		return err
	}

	m.scriptsMu.Lock()
	defer m.scriptsMu.Unlock()

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
