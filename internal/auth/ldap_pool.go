package auth

import (
	"errors"
	"sync"
)

// defaultLDAPPoolSize is the number of LDAP connections kept warm by default.
const defaultLDAPPoolSize = 10

// errPoolClosed is returned by acquire when the pool has already been shut down.
var errPoolClosed = errors.New("ldap pool is closed")

// pooledLDAPConn is the minimal surface ldapPool needs from a connection.
// *ldap.Conn satisfies this interface directly; tests can substitute a fake.
type pooledLDAPConn interface {
	Close() error
	IsClosing() bool
}

// ldapDialer creates a fresh authenticated-ready connection.
type ldapDialer func() (pooledLDAPConn, error)

// ldapPool keeps a bounded set of warm LDAP connections to amortize the
// TLS handshake and TCP setup cost across many short-lived auth operations.
//
// Semantics:
//   - acquire() returns a healthy conn from the pool, or dials a new one.
//   - release(conn) returns a healthy conn to the pool, or closes it if
//     the pool is full or the conn is no longer usable.
//   - close() drains the pool and refuses further acquires.
type ldapPool struct {
	dialer  ldapDialer
	maxSize int
	conns   chan pooledLDAPConn

	mu     sync.Mutex
	closed bool
}

// newLDAPPool creates a pool that keeps up to maxSize warm connections.
// A maxSize <= 0 falls back to defaultLDAPPoolSize.
func newLDAPPool(dialer ldapDialer, maxSize int) *ldapPool {
	if maxSize <= 0 {
		maxSize = defaultLDAPPoolSize
	}
	return &ldapPool{
		dialer:  dialer,
		maxSize: maxSize,
		conns:   make(chan pooledLDAPConn, maxSize),
	}
}

// acquire returns a usable connection. It drains stale conns from the
// channel before dialing a fresh one.
func (p *ldapPool) acquire() (pooledLDAPConn, error) {
	for {
		select {
		case conn, ok := <-p.conns:
			if !ok {
				return nil, errPoolClosed
			}
			if conn != nil && !conn.IsClosing() {
				return conn, nil
			}
			if conn != nil {
				_ = conn.Close()
			}
		default:
			p.mu.Lock()
			closed := p.closed
			p.mu.Unlock()
			if closed {
				return nil, errPoolClosed
			}
			return p.dialer()
		}
	}
}

// release returns a healthy connection to the pool. Connections that are
// closing, or that arrive after the pool is closed, are closed instead.
// Non-blocking: if the pool buffer is full, the connection is closed.
func (p *ldapPool) release(conn pooledLDAPConn) {
	if conn == nil {
		return
	}
	if conn.IsClosing() {
		_ = conn.Close()
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = conn.Close()
		return
	}
	select {
	case p.conns <- conn:
	default:
		_ = conn.Close()
	}
}

// discard unconditionally closes a connection without returning it to the pool.
// Use this when an operation left the conn in an unknown state (e.g. mid-bind).
func (p *ldapPool) discard(conn pooledLDAPConn) {
	if conn == nil {
		return
	}
	_ = conn.Close()
}

// close drains the pool and prevents future acquires.
func (p *ldapPool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	close(p.conns)
	for conn := range p.conns {
		if conn != nil {
			_ = conn.Close()
		}
	}
}
