package av

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// TestScanFoundWithNoColon tests the branch where the FOUND response
// has no colon, resulting in virus name "unknown"
func TestScanFoundWithNoColon(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		return "FOUND"
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
		Action:  "reject",
	})

	result, err := scanner.Scan([]byte("test"))
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if !result.Infected {
		t.Error("Expected infected result")
	}
	if result.Virus != "unknown" {
		t.Errorf("Expected virus name 'unknown', got: %s", result.Virus)
	}
}

// TestScanEmptyData tests scanning empty data
func TestScanEmptyData(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		return "stream: OK"
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	result, err := scanner.Scan([]byte{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if result.Infected {
		t.Error("Expected clean result for empty data")
	}
}

// TestPingConnectionRefused tests Ping when server is not reachable
func TestPingConnectionRefused(t *testing.T) {
	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    "127.0.0.1:1",
		Timeout: 1 * time.Second,
	})

	err := scanner.Ping()
	if err == nil {
		t.Error("Expected error when pinging unreachable server")
	}
}

// TestScanVersionConnectionRefused tests ScanVersion when server is not reachable
func TestScanVersionConnectionRefused(t *testing.T) {
	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    "127.0.0.1:1",
		Timeout: 1 * time.Second,
	})

	_, err := scanner.ScanVersion()
	if err == nil {
		t.Error("Expected error when version-checking unreachable server")
	}
}

// startClosingClamAV starts a server that closes connection immediately after
// receiving the command, to test read/write error paths.
func startClosingClamAV(t *testing.T) (addr string, cleanup func()) {
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
				// Read the command then close immediately
				reader.ReadString('\x00')
			}(conn)
		}
	}()

	return ln.Addr().String(), func() {
		ln.Close()
		<-done
	}
}

func TestScanVersionWithServerClose(t *testing.T) {
	addr, cleanup := startClosingClamAV(t)
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	_, err := scanner.ScanVersion()
	if err == nil {
		t.Error("Expected error when server closes connection")
	}
}

func TestPingWithServerClose(t *testing.T) {
	addr, cleanup := startClosingClamAV(t)
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	err := scanner.Ping()
	if err == nil {
		t.Error("Expected error when server closes connection")
	}
}

// startSlowClamAV starts a server that reads the command but never responds,
// to trigger the read deadline timeout in Scan.
func startSlowClamAV(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
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
				reader.ReadString('\x00')
				// Block until test cleanup runs
				<-ctx.Done()
			}(conn)
		}
	}()

	return ln.Addr().String(), func() {
		cancel()
		ln.Close()
		<-done
	}
}

func TestScanReadTimeout(t *testing.T) {
	addr, cleanup := startSlowClamAV(t)
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 100 * time.Millisecond,
	})

	_, err := scanner.Scan([]byte("test"))
	if err == nil {
		t.Error("Expected error when read times out")
	}
}

func TestScanVersionReadClose(t *testing.T) {
	addr, cleanup := startClosingClamAV(t)
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	_, err := scanner.ScanVersion()
	if err == nil {
		t.Error("Expected error when server closes connection during version read")
	}
}

func TestPingReadClose(t *testing.T) {
	addr, cleanup := startClosingClamAV(t)
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	err := scanner.Ping()
	if err == nil {
		t.Error("Expected error when server closes connection during ping read")
	}
}

// startReadCloseClamAV starts a server that reads the INSTREAM data
// but closes before sending response.
func startReadCloseClamAV(t *testing.T) (addr string, cleanup func()) {
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
				c.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(c)
				line, _ := reader.ReadString('\x00')
				cmd := strings.TrimRight(line, "\x00")
				if strings.HasPrefix(cmd, "zINSTREAM") {
					var chunkLen uint32
					for {
						if err := readChunkHelper(reader, &chunkLen); err != nil {
							return
						}
						if chunkLen == 0 {
							break
						}
					}
					// Close without responding
				}
			}(conn)
		}
	}()

	return ln.Addr().String(), func() {
		ln.Close()
		<-done
	}
}

func readChunkHelper(reader *bufio.Reader, chunkLen *uint32) error {
	buf := make([]byte, 4)
	if _, err := reader.Read(buf); err != nil {
		return err
	}
	*chunkLen = uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
	if *chunkLen > 0 {
		data := make([]byte, *chunkLen)
		if _, err := reader.Read(data); err != nil {
			return err
		}
	}
	return nil
}

func TestScanReadResponseFail(t *testing.T) {
	addr, cleanup := startReadCloseClamAV(t)
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	_, err := scanner.Scan([]byte("test data"))
	if err == nil {
		t.Error("Expected error when server closes without response")
	}
}

// TestScanFoundWithVirusName tests the normal FOUND response parsing
func TestScanFoundWithVirusName(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		return "stream: Win.Trojan.Agent FOUND"
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	result, err := scanner.Scan([]byte("malware"))
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if !result.Infected {
		t.Error("Expected infected result")
	}
	if result.Virus != "Win.Trojan.Agent" {
		t.Errorf("Expected virus 'Win.Trojan.Agent', got: %s", result.Virus)
	}
}

// TestScanResponseNotOKNotFound tests a response that is neither OK nor FOUND nor ERROR
func TestScanResponseNotOKNotFound(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		return "stream: SOME_RANDOM_STATUS"
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	result, err := scanner.Scan([]byte("test"))
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	// Should return clean (not infected, no error)
	if result.Infected {
		t.Error("Expected clean result for unknown response status")
	}
}

// TestScanVersionWithValidResponse tests version with a different response format
func TestScanVersionWithValidResponse(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		return fmt.Sprintf("ClamAV 0.103.6/26634/%s", time.Now().Format("Mon Jan 2 15:04:05 2006"))
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	version, err := scanner.ScanVersion()
	if err != nil {
		t.Fatalf("ScanVersion failed: %v", err)
	}
	if !strings.Contains(version, "ClamAV") {
		t.Errorf("Expected ClamAV version string, got: %s", version)
	}
}
