package av

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNewScanner(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Addr:    "127.0.0.1:3310",
		Timeout: 10 * time.Second,
		Action:  "reject",
	}

	scanner := NewScanner(cfg)

	if !scanner.IsEnabled() {
		t.Error("Expected scanner to be enabled")
	}
	if scanner.Action() != "reject" {
		t.Errorf("Expected action 'reject', got: %s", scanner.Action())
	}
}

func TestNewScannerDefaults(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Addr:    "127.0.0.1:3310",
	}

	scanner := NewScanner(cfg)

	if scanner.timeout != 30*time.Second {
		t.Errorf("Expected default timeout 30s, got: %v", scanner.timeout)
	}
	if scanner.action != "reject" {
		t.Errorf("Expected default action 'reject', got: %s", scanner.action)
	}
}

func TestScannerDisabled(t *testing.T) {
	cfg := Config{
		Enabled: false,
	}

	scanner := NewScanner(cfg)

	if scanner.IsEnabled() {
		t.Error("Expected scanner to be disabled")
	}

	// Scan should return clean when disabled
	result, err := scanner.Scan([]byte("test data"))
	if err != nil {
		t.Errorf("Scan on disabled scanner returned error: %v", err)
	}
	if result.Infected {
		t.Error("Disabled scanner should not report infected")
	}
}

func TestScannerNoAddress(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Addr:    "",
	}

	scanner := NewScanner(cfg)

	if scanner.IsEnabled() {
		t.Error("Scanner with empty address should not be enabled")
	}
}

func TestScanResult(t *testing.T) {
	result := &ScanResult{
		Infected: true,
		Virus:    "EICAR-Test-File",
	}

	if !result.Infected {
		t.Error("Expected infected to be true")
	}
	if result.Virus != "EICAR-Test-File" {
		t.Errorf("Expected virus name 'EICAR-Test-File', got: %s", result.Virus)
	}
}

func TestScanResultClean(t *testing.T) {
	result := &ScanResult{
		Infected: false,
		Virus:    "",
	}

	if result.Infected {
		t.Error("Expected infected to be false")
	}
	if result.Virus != "" {
		t.Errorf("Expected empty virus name, got: %s", result.Virus)
	}
}

func TestPingNotEnabled(t *testing.T) {
	scanner := NewScanner(Config{Enabled: false})
	err := scanner.Ping()
	if err == nil {
		t.Error("Expected error when pinging disabled scanner")
	}
}

func TestVersionNotEnabled(t *testing.T) {
	scanner := NewScanner(Config{Enabled: false})
	_, err := scanner.ScanVersion()
	if err == nil {
		t.Error("Expected error when querying version on disabled scanner")
	}
}

func TestScanNotEnabled(t *testing.T) {
	scanner := NewScanner(Config{Enabled: false})
	result, err := scanner.Scan([]byte("test"))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.Infected {
		t.Error("Disabled scanner should return clean result")
	}
}

func TestAllActions(t *testing.T) {
	actions := []string{"reject", "quarantine", "tag"}
	for _, action := range actions {
		cfg := Config{
			Enabled: true,
			Addr:    "127.0.0.1:3310",
			Action:  action,
		}
		scanner := NewScanner(cfg)
		if scanner.Action() != action {
			t.Errorf("Expected action %q, got: %q", action, scanner.Action())
		}
	}
}

// startFakeClamAV starts a fake ClamAV server on a random port.
func startFakeClamAV(t *testing.T, handler func(cmd string) string) (addr string, cleanup func()) {
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
				for {
					line, err := reader.ReadString('\x00')
					if err != nil {
						return
					}
					cmd := strings.TrimRight(line, "\x00")

					if strings.HasPrefix(cmd, "zINSTREAM") {
						for {
							var chunkLen uint32
							if err := binary.Read(reader, binary.BigEndian, &chunkLen); err != nil {
								return
							}
							if chunkLen == 0 {
								break
							}
							buf := make([]byte, chunkLen)
							if _, err := io.ReadFull(reader, buf); err != nil {
								return
							}
						}
						resp := handler("INSTREAM")
						fmt.Fprintf(c, "%s\n", resp)
					} else {
						resp := handler(cmd)
						fmt.Fprintf(c, "%s\n", resp)
					}
				}
			}(conn)
		}
	}()

	return ln.Addr().String(), func() {
		ln.Close()
		<-done
	}
}

func TestScanWithFakeServerClean(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		return "stream: OK"
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
		Action:  "reject",
	})

	result, err := scanner.Scan([]byte("clean data"))
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if result.Infected {
		t.Error("Expected clean result")
	}
}

func TestScanWithFakeServerInfected(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		return "stream: EICAR-Test-File FOUND"
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
		Action:  "reject",
	})

	result, err := scanner.Scan([]byte("EICAR test string"))
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if !result.Infected {
		t.Error("Expected infected result")
	}
	if result.Virus != "EICAR-Test-File" {
		t.Errorf("Expected virus 'EICAR-Test-File', got: %s", result.Virus)
	}
}

func TestScanWithFakeServerError(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		return "stream: ERROR"
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
		Action:  "reject",
	})

	_, err := scanner.Scan([]byte("test"))
	if err == nil {
		t.Error("Expected error from ClamAV ERROR response")
	}
}

func TestPingWithFakeServer(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		if cmd == "zPING" {
			return "PONG"
		}
		return ""
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	if err := scanner.Ping(); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

func TestPingWithFakeServerBadResponse(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		return "NOT PONG"
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 5 * time.Second,
	})

	err := scanner.Ping()
	if err == nil {
		t.Error("Expected error from bad PONG response")
	}
}

func TestVersionWithFakeServer(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		if cmd == "zVERSION" {
			return "ClamAV 1.0.0/12345/Tue Jan 1 00:00:00 2024"
		}
		return ""
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

func TestScanConnectionRefused(t *testing.T) {
	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    "127.0.0.1:1",
		Timeout: 1 * time.Second,
	})

	_, err := scanner.Scan([]byte("test"))
	if err == nil {
		t.Error("Expected error when connecting to invalid address")
	}
}

func TestScanLargeDataWithFakeServer(t *testing.T) {
	addr, cleanup := startFakeClamAV(t, func(cmd string) string {
		return "stream: OK"
	})
	defer cleanup()

	scanner := NewScanner(Config{
		Enabled: true,
		Addr:    addr,
		Timeout: 10 * time.Second,
		Action:  "tag",
	})

	largeData := make([]byte, 100000)
	for i := range largeData {
		largeData[i] = byte('A' + (i % 26))
	}

	result, err := scanner.Scan(largeData)
	if err != nil {
		t.Fatalf("Scan of large data failed: %v", err)
	}
	if result.Infected {
		t.Error("Expected clean result for large data")
	}
}
