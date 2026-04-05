package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

// State represents the circuit breaker state
type State int

const (
	StateClosed State = iota   // Normal operation
	StateOpen                  // Failing, rejecting requests
	StateHalfOpen              // Testing if service recovered
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Config holds circuit breaker configuration
type Config struct {
	MaxFailures     int           // Number of failures before opening
	Timeout         time.Duration // Duration to stay open before half-open
	SuccessThreshold int          // Success count to close from half-open
	FailureThreshold int          // Failure count to open from half-open
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		MaxFailures:      5,
		Timeout:          30 * time.Second,
		SuccessThreshold: 2,
		FailureThreshold: 2,
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	config Config
	state  State
	mutex  sync.RWMutex

	failures    int
	successes   int
	lastFailure time.Time
	halfOpenReq int // Count of requests in half-open state
}

// New creates a new circuit breaker
func New(config Config) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
		state:  StateClosed,
	}
}

// NewDefault creates a circuit breaker with default config
func NewDefault() *CircuitBreaker {
	return New(DefaultConfig())
}

// State returns the current state
func (cb *CircuitBreaker) State() State {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// Allow returns true if the request should be allowed
func (cb *CircuitBreaker) Allow() bool {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	switch cb.state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailure) > cb.config.Timeout {
			cb.state = StateHalfOpen
			cb.failures = 0
			cb.successes = 0
			cb.halfOpenReq = 0
			return true
		}
		return false

	case StateHalfOpen:
		// Limit concurrent requests in half-open state
		if cb.halfOpenReq < cb.config.SuccessThreshold {
			cb.halfOpenReq++
			return true
		}
		return false

	default:
		return false
	}
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	switch cb.state {
	case StateClosed:
		cb.failures = 0 // Reset failures on success

	case StateHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.state = StateClosed
			cb.failures = 0
			cb.successes = 0
			cb.halfOpenReq = 0
		}
	}
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.lastFailure = time.Now()

	switch cb.state {
	case StateClosed:
		cb.failures++
		if cb.failures >= cb.config.MaxFailures {
			cb.state = StateOpen
		}

	case StateHalfOpen:
		cb.failures++
		if cb.failures >= cb.config.FailureThreshold {
			cb.state = StateOpen
		}
	}
}

// Execute runs the given function if the circuit allows it
// Returns ErrCircuitOpen if the circuit is open
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.Allow() {
		return ErrCircuitOpen
	}

	err := fn()
	if err != nil {
		cb.RecordFailure()
		return err
	}

	cb.RecordSuccess()
	return nil
}

// Metrics returns current circuit breaker metrics
func (cb *CircuitBreaker) Metrics() Metrics {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	return Metrics{
		State:           cb.state.String(),
		Failures:        cb.failures,
		Successes:       cb.successes,
		LastFailure:     cb.lastFailure,
		HalfOpenRequests: cb.halfOpenReq,
	}
}

// Metrics holds circuit breaker statistics
type Metrics struct {
	State            string
	Failures         int
	Successes        int
	LastFailure      time.Time
	HalfOpenRequests int
}

// Common errors
var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

// Manager manages multiple circuit breakers
type Manager struct {
	breakers map[string]*CircuitBreaker
	mutex    sync.RWMutex
}

// NewManager creates a new circuit breaker manager
func NewManager() *Manager {
	return &Manager{
		breakers: make(map[string]*CircuitBreaker),
	}
}

// Get returns a circuit breaker by name, creating if needed
func (m *Manager) Get(name string, config ...Config) *CircuitBreaker {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if cb, ok := m.breakers[name]; ok {
		return cb
	}

	var cfg Config
	if len(config) > 0 {
		cfg = config[0]
	} else {
		cfg = DefaultConfig()
	}

	cb := New(cfg)
	m.breakers[name] = cb
	return cb
}

// Remove removes a circuit breaker
func (m *Manager) Remove(name string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.breakers, name)
}

// AllMetrics returns metrics for all circuit breakers
func (m *Manager) AllMetrics() map[string]Metrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]Metrics, len(m.breakers))
	for name, cb := range m.breakers {
		result[name] = cb.Metrics()
	}
	return result
}
