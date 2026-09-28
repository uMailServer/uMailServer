package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

// State represents the circuit breaker state
type State int

const (
	StateClosed   State = iota // Normal operation
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
	MaxFailures      int           // Number of failures before opening
	Timeout          time.Duration // Duration to stay open before half-open
	SuccessThreshold int           // Success count to close from half-open
	FailureThreshold int           // Failure count to open from half-open
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
	halfOpenReq int    // Count of requests in half-open state
	halfOpenGen uint64 // Id of the current half-open round; bumped on each open->half-open transition
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
			// Start a new half-open round. Bumping the id here is what lets a
			// completion tell whether it belongs to this round or to a probe
			// still in flight from the previous one, so a straggler cannot
			// release a slot that belongs to the new round.
			cb.halfOpenGen++
			cb.halfOpenReq = 0
			// Count the probe this transition admits. Without the increment the
			// counter still reads 0, so the next Allow() admits a further probe
			// and the round runs SuccessThreshold+1 concurrent probes rather
			// than the documented cap.
			cb.halfOpenReq++
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

// RecordSuccess records a successful operation.
//
// In half-open it deliberately does NOT release an admission slot. This method
// carries no half-open round id, so the breaker cannot tell whether the caller
// is a probe admitted in the current round or a straggler still in flight from
// the previous one; releasing on a straggler's behalf would free a live probe's
// slot and re-exceed the concurrent cap. Callers that admitted the probe
// themselves should use Execute (or recordSuccess) for exact slot accounting.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.recordSuccess(0, false)
}

// recordSuccess applies a successful result. When release is true the caller
// supplied the half-open round id it was admitted in (gen), and an admission
// slot is released only if gen still names the current round — so a completion
// carried over from a previous round cannot decrement this round's halfOpenReq.
func (cb *CircuitBreaker) recordSuccess(gen uint64, release bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	switch cb.state {
	case StateClosed:
		cb.failures = 0 // Reset failures on success

	case StateHalfOpen:
		// Release this in-flight probe slot. halfOpenReq tracks *concurrent*
		// probes (see Allow); without decrementing on completion it becomes a
		// lifetime cap and the breaker wedges after any mixed round that
		// crosses neither threshold.
		if release && gen == cb.halfOpenGen && cb.halfOpenReq > 0 {
			cb.halfOpenReq--
		}
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.state = StateClosed
			cb.failures = 0
			cb.successes = 0
			cb.halfOpenReq = 0
		}
	}
}

// RecordFailure records a failed operation.
//
// As with RecordSuccess, it does not release a half-open admission slot: it
// carries no round id, so a straggler from a previous round cannot be
// distinguished from a probe admitted in the current one. Use Execute (or
// recordFailure) when the caller knows which round admitted the probe.
func (cb *CircuitBreaker) RecordFailure() {
	cb.recordFailure(0, false)
}

// recordFailure applies a failed result. When release is true the caller
// supplied the half-open round id it was admitted in (gen), and an admission
// slot is released only if gen still names the current round. A failed probe
// occupies a slot exactly like a successful one.
func (cb *CircuitBreaker) recordFailure(gen uint64, release bool) {
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
		// Release this in-flight probe slot (see recordSuccess).
		if release && gen == cb.halfOpenGen && cb.halfOpenReq > 0 {
			cb.halfOpenReq--
		}
		cb.failures++
		if cb.failures >= cb.config.FailureThreshold {
			cb.state = StateOpen
		}
	}
}

// Execute runs the given function if the circuit allows it
// Returns ErrCircuitOpen if the circuit is open
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if fn == nil {
		return errors.New("circuit breaker: nil function")
	}
	if !cb.Allow() {
		return ErrCircuitOpen
	}

	// Capture the half-open round this request was admitted in. If the
	// breaker re-opens and starts a new round while fn is still running, the
	// completion below will not match halfOpenGen and will correctly decline
	// to release a slot belonging to the new round.
	gen := cb.currentHalfOpenGen()

	err := fn()
	if err != nil {
		cb.recordFailure(gen, true)
		return err
	}

	cb.recordSuccess(gen, true)
	return nil
}

// currentHalfOpenGen returns the id of the current half-open round.
func (cb *CircuitBreaker) currentHalfOpenGen() uint64 {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.halfOpenGen
}

// Metrics returns current circuit breaker metrics
func (cb *CircuitBreaker) Metrics() Metrics {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	return Metrics{
		State:            cb.state.String(),
		Failures:         cb.failures,
		Successes:        cb.successes,
		LastFailure:      cb.lastFailure,
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
