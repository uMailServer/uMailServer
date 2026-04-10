package server

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// ConnectionDrainer manages graceful connection draining during shutdown
type ConnectionDrainer struct {
	activeConnections int64
	maxWaitTime       time.Duration
	forceCloseAfter   time.Duration
	mu                sync.Mutex
	closed            bool
	closeCh           chan struct{}
}

// NewConnectionDrainer creates a new connection drainer
func NewConnectionDrainer(maxWait, forceClose time.Duration) *ConnectionDrainer {
	return &ConnectionDrainer{
		maxWaitTime:     maxWait,
		forceCloseAfter: forceClose,
		closeCh:         make(chan struct{}),
	}
}

// AddConnection increments the active connection counter
// Returns false if the drainer is already closed
func (cd *ConnectionDrainer) AddConnection() bool {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	if cd.closed {
		return false
	}

	atomic.AddInt64(&cd.activeConnections, 1)
	return true
}

// RemoveConnection decrements the active connection counter
func (cd *ConnectionDrainer) RemoveConnection() {
	atomic.AddInt64(&cd.activeConnections, -1)
}

// ActiveConnections returns the current number of active connections
func (cd *ConnectionDrainer) ActiveConnections() int64 {
	return atomic.LoadInt64(&cd.activeConnections)
}

// Close initiates the draining process
// It returns immediately if there are no connections
// Otherwise, it waits for connections to drain or timeout
func (cd *ConnectionDrainer) Close(ctx context.Context) error {
	cd.mu.Lock()
	cd.closed = true
	close(cd.closeCh)
	cd.mu.Unlock()

	// Create a timeout context if not provided
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), cd.maxWaitTime)
		defer cancel()
	}

	// Wait for connections to drain
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if cd.ActiveConnections() == 0 {
				return nil
			}
		}
	}
}

// Wait waits for all connections to close
func (cd *ConnectionDrainer) Wait() {
	<-cd.closeCh

	// Wait for active connections to reach 0 or timeout
	timeout := time.After(cd.forceCloseAfter)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return
		case <-ticker.C:
			if cd.ActiveConnections() == 0 {
				return
			}
		}
	}
}

// ServerComponent interface for components that can be drained
type ServerComponent interface {
	Stop() error
	ActiveConnections() int
}

// GracefulShutdown coordinates shutdown of multiple components
type GracefulShutdown struct {
	components []ServerComponent
	drainers   []*ConnectionDrainer
	timeout    time.Duration
}

// NewGracefulShutdown creates a new graceful shutdown coordinator
func NewGracefulShutdown(timeout time.Duration) *GracefulShutdown {
	return &GracefulShutdown{
		components: make([]ServerComponent, 0),
		drainers:   make([]*ConnectionDrainer, 0),
		timeout:    timeout,
	}
}

// AddComponent adds a server component to be shut down
func (gs *GracefulShutdown) AddComponent(_ string, component ServerComponent) {
	gs.components = append(gs.components, component)
}

// AddDrainer adds a connection drainer
func (gs *GracefulShutdown) AddDrainer(drainer *ConnectionDrainer) {
	gs.drainers = append(gs.drainers, drainer)
}

// Shutdown performs graceful shutdown of all components
// 1. Stop accepting new connections
// 2. Wait for existing connections to drain
// 3. Force close remaining connections after timeout
func (gs *GracefulShutdown) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), gs.timeout)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(gs.components)+len(gs.drainers))

	// Drain all connection drainers first
	for _, drainer := range gs.drainers {
		wg.Add(1)
		go func(d *ConnectionDrainer) {
			defer wg.Done()
			if err := d.Close(ctx); err != nil {
				errCh <- err
			}
		}(drainer)
	}

	// Wait for drainers to finish
	wg.Wait()

	// Stop all components
	for _, component := range gs.components {
		wg.Add(1)
		go func(c ServerComponent) {
			defer wg.Done()
			if err := c.Stop(); err != nil {
				errCh <- err
			}
		}(component)
	}

	// Wait for all components to stop or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All components stopped gracefully
	case <-ctx.Done():
		// Timeout reached
		errCh <- ctx.Err()
	}

	close(errCh)

	// Collect errors
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errs[0] // Return first error
	}

	return nil
}
