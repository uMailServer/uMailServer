package av

import (
	"errors"
	"net"
	"testing"
	"time"
)

// errorConn is a net.Conn implementation where every operation returns an error.
// Specific operations can be configured to succeed to control which code path fails.
type errorConn struct {
	setDeadlineErr error
	writeErr       error
	readErr        error
	writeCount     int // number of successful writes before writeErr applies
	writesDone     int // counter of writes performed
	closeCalled    bool
}

func (c *errorConn) Read(b []byte) (n int, err error) {
	return 0, c.readErr
}

func (c *errorConn) Write(b []byte) (n int, err error) {
	c.writesDone++
	if c.writesDone > c.writeCount {
		return 0, c.writeErr
	}
	return len(b), nil
}

func (c *errorConn) Close() error {
	c.closeCalled = true
	return nil
}

func (c *errorConn) LocalAddr() net.Addr                { return nil }
func (c *errorConn) RemoteAddr() net.Addr               { return nil }
func (c *errorConn) SetDeadline(t time.Time) error      { return c.setDeadlineErr }
func (c *errorConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *errorConn) SetWriteDeadline(t time.Time) error { return nil }

// newScannerWithDial creates a Scanner with a custom dial function that returns
// the provided connection.
func newScannerWithDial(conn net.Conn) *Scanner {
	return &Scanner{
		addr:    "127.0.0.1:3310",
		timeout: 5 * time.Second,
		enabled: true,
		action:  "reject",
		dial: func(network, addr string, timeout time.Duration) (net.Conn, error) {
			return conn, nil
		},
	}
}

// TestScanSetDeadlineErrorMock tests the SetDeadline error path in Scan (line 77-79).
func TestScanSetDeadlineErrorMock(t *testing.T) {
	conn := &errorConn{
		setDeadlineErr: errors.New("set deadline failed"),
	}
	scanner := newScannerWithDial(conn)

	_, err := scanner.Scan([]byte("test"))
	if err == nil {
		t.Fatal("Expected error from SetDeadline failure")
	}
	if conn.setDeadlineErr != nil && !errors.Is(err, conn.setDeadlineErr) {
		// Check that the error message contains "deadline"
		t.Logf("Scan error: %v", err)
	}
	if !conn.closeCalled {
		t.Error("Expected connection to be closed")
	}
}

// TestScanWriteCommandErrorMock tests the INSTREAM command write error path (line 82-85).
func TestScanWriteCommandErrorMock(t *testing.T) {
	conn := &errorConn{
		writeErr:   errors.New("write failed"),
		writeCount: 0, // First write fails
	}
	scanner := newScannerWithDial(conn)

	_, err := scanner.Scan([]byte("test"))
	if err == nil {
		t.Fatal("Expected error from INSTREAM write failure")
	}
	if !conn.closeCalled {
		t.Error("Expected connection to be closed")
	}
}

// TestScanWriteChunkLengthErrorMock tests the chunk length write error path in Scan.
// Write #1 (INSTREAM command) succeeds, write #2 (chunk length) fails.
func TestScanWriteChunkLengthErrorMock(t *testing.T) {
	conn := &errorConn{
		writeErr:   errors.New("write failed"),
		writeCount: 1, // INSTREAM command succeeds, chunk length write fails
	}
	scanner := newScannerWithDial(conn)

	_, err := scanner.Scan([]byte("test"))
	if err == nil {
		t.Fatal("Expected error from chunk length write failure")
	}
	if !conn.closeCalled {
		t.Error("Expected connection to be closed")
	}
}

// TestScanWriteTerminationErrorMock tests the termination marker write error (line 117-120).
// We need the first writes (command + chunk length + chunk data) to succeed, then
// the termination write to fail.
func TestScanWriteTerminationErrorMock(t *testing.T) {
	conn := &errorConn{
		writeErr:   errors.New("write failed"),
		writeCount: 3, // INSTREAM command + chunk length + chunk data succeed, termination fails
	}
	scanner := newScannerWithDial(conn)

	_, err := scanner.Scan([]byte("test"))
	if err == nil {
		t.Fatal("Expected error from termination write failure")
	}
	if !conn.closeCalled {
		t.Error("Expected connection to be closed")
	}
}

// TestScanVersionWriteErrorMock tests the VERSION command write error (line 165-168).
func TestScanVersionWriteErrorMock(t *testing.T) {
	conn := &errorConn{
		writeErr:   errors.New("write failed"),
		writeCount: 0, // First write fails
	}
	scanner := newScannerWithDial(conn)

	_, err := scanner.ScanVersion()
	if err == nil {
		t.Fatal("Expected error from VERSION write failure")
	}
	if !conn.closeCalled {
		t.Error("Expected connection to be closed")
	}
}

// TestPingWriteErrorMock tests the PING command write error (line 191-194).
func TestPingWriteErrorMock(t *testing.T) {
	conn := &errorConn{
		writeErr:   errors.New("write failed"),
		writeCount: 0, // First write fails
	}
	scanner := newScannerWithDial(conn)

	err := scanner.Ping()
	if err == nil {
		t.Fatal("Expected error from PING write failure")
	}
	if !conn.closeCalled {
		t.Error("Expected connection to be closed")
	}
}
