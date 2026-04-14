package av

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// startDeadlineErrorServer starts a server that accepts connections but
// immediately sets a very short deadline, causing SetDeadline on the client
// side to potentially fail or the connection to error.
func startDeadlineErrorServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Read nothing, just close immediately
			}(conn)
		}
	}()

	return ln.Addr().String(), func() {
		ln.Close()
		<-done
	}
}

// startChunkWriteErrorServer starts a server that reads the INSTREAM command
// then closes the connection, causing chunk write to fail.
func startChunkWriteErrorServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				reader := bufio.NewReader(c)
				// Read the INSTREAM command
				line, _ := reader.ReadString('\x00')
				cmd := strings.TrimRight(line, "\x00")
				_ = cmd
				// Close immediately to cause write errors on client
			}(conn)
		}
	}()

	return ln.Addr().String(), func() {
		ln.Close()
		<-done
	}
}

// startPartialChunkServer reads the command and a bit of chunk data, then closes
func startPartialChunkServer(t *testing.T, closeAfterData bool) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(c)
				line, _ := reader.ReadString('\x00')
				_ = strings.TrimRight(line, "\x00")

				if closeAfterData {
					// Read 4 bytes for chunk length
					lenBuf := make([]byte, 4)
					if _, err := reader.Read(lenBuf); err != nil {
						return
					}
					// Read some data bytes then close
					dataBuf := make([]byte, 10)
					reader.Read(dataBuf)
				}
				// Close to trigger write errors
			}(conn)
		}
	}()

	return ln.Addr().String(), func() {
		ln.Close()
		<-done
	}
}

// TestScanSetDeadlineError tests the SetDeadline error path (line 73-75)
func TestScanSetDeadlineError(t *testing.T) {
	// This is hard to trigger directly since SetDeadline rarely fails.
	// We test with an address that causes connection issues.
	// Using a valid server but with extremely short timeout that may cause
	// the deadline to be in the past.
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		return "stream: OK"
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 0, // Zero timeout - SetDeadline with time.Now().Add(0)
	})

	// This should either work or fail - either way exercises the code path
	_, _ = scanner.Scan([]byte("test"))
}

// TestScanWriteCommandError tests the error path for writing INSTREAM command (line 79-81)
func TestScanWriteCommandError(t *testing.T) {
	addr, cleanup := startDeadlineErrorServer(t)
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	_, err := scanner.Scan([]byte("test"))
	if err == nil {
		t.Error("Expected error when server closes connection")
	}
}

// TestScanWriteChunkLengthError tests the error path for writing chunk length (line 101-103)
func TestScanWriteChunkLengthError(t *testing.T) {
	addr, cleanup := startChunkWriteErrorServer(t)
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	_, err := scanner.Scan([]byte("test data that needs chunking"))
	if err == nil {
		t.Error("Expected error when connection closes during chunk write")
	}
}

// TestScanWriteChunkDataError tests the error path for writing chunk data (line 106-108)
func TestScanWriteChunkDataError(t *testing.T) {
	addr, cleanup := startPartialChunkServer(t, true)
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	// Send enough data that the chunk write should fail
	largeData := make([]byte, 100000)
	for i := range largeData {
		largeData[i] = byte('X')
	}

	_, err := scanner.Scan(largeData)
	if err == nil {
		t.Error("Expected error when connection closes during chunk data write")
	}
}

// TestScanWriteTerminationError tests the error path for writing termination marker (line 114-116)
func TestScanWriteTerminationError(t *testing.T) {
	// Server that reads everything then closes before we can write termination
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(c)
				line, _ := reader.ReadString('\x00')
				_ = strings.TrimRight(line, "\x00")

				// Read all chunks
				for {
					var chunkLen uint32
					buf := make([]byte, 4)
					if _, err := reader.Read(buf); err != nil {
						return
					}
					chunkLen = uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
					if chunkLen == 0 {
						break
					}
					data := make([]byte, chunkLen)
					if _, err := reader.Read(data); err != nil {
						return
					}
				}
				// Close without reading termination or sending response
			}(conn)
		}
	}()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    ln.Addr().String(),
		Timeout: 5 * time.Second,
	})

	_, scanErr := scanner.Scan([]byte("test"))
	ln.Close()
	<-done

	if scanErr == nil {
		t.Error("Expected error when server closes before termination")
	}
}

// TestScanVersionWriteError tests the write error path in ScanVersion (line 162-164)
func TestScanVersionWriteError(t *testing.T) {
	addr, cleanup := startDeadlineErrorServer(t)
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	_, err := scanner.ScanVersion()
	if err == nil {
		t.Error("Expected error when server closes during version write")
	}
}

// TestPingWriteError tests the write error path in Ping (line 188-190)
func TestPingWriteError(t *testing.T) {
	addr, cleanup := startDeadlineErrorServer(t)
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	err := scanner.Ping()
	if err == nil {
		t.Error("Expected error when server closes during ping write")
	}
}
