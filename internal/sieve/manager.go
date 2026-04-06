package sieve

import (
	"sync"
	"time"
)

// Manager handles Sieve script storage and execution
type Manager struct {
	scripts   map[string]*Script // userID -> active script
	scriptsMu sync.RWMutex

	// Vacation cache: prevents spamming the same sender
	vacationCache  map[string]time.Time
	vacationCacheMu sync.Mutex
}

// NewManager creates a new Sieve manager
func NewManager() *Manager {
	return &Manager{
		scripts:       make(map[string]*Script),
		vacationCache: make(map[string]time.Time),
	}
}

// CompileScript compiles a Sieve script string and returns the Script
func (m *Manager) CompileScript(source string) (*Script, error) {
	p := NewParser(source)
	return p.Parse()
}

// SetActiveScript sets the active script for a user
func (m *Manager) SetActiveScript(userID string, source string) error {
	script, err := m.CompileScript(source)
	if err != nil {
		return err
	}

	m.scriptsMu.Lock()
	defer m.scriptsMu.Unlock()
	m.scripts[userID] = script
	return nil
}

// GetActiveScript gets the active script for a user
func (m *Manager) GetActiveScript(userID string) (*Script, bool) {
	m.scriptsMu.RLock()
	defer m.scriptsMu.RUnlock()
	script, ok := m.scripts[userID]
	return script, ok
}

// HasActiveScript returns true if user has a sieve script
func (m *Manager) HasActiveScript(userID string) bool {
	m.scriptsMu.RLock()
	defer m.scriptsMu.RUnlock()
	_, ok := m.scripts[userID]
	return ok
}

// DeleteScript removes the active script for a user
func (m *Manager) DeleteScript(userID string) {
	m.scriptsMu.Lock()
	defer m.scriptsMu.Unlock()
	delete(m.scripts, userID)
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
	m.vacationCache[sender] = time.Now()
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
