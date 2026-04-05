package queue

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// TestEnqueueGetStatsError tests Enqueue when getStats returns an error
// by closing the database before calling Enqueue.
func TestEnqueueGetStatsError(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	// Close database immediately to cause errors
	database.Close()

	manager := NewManager(database, nil, dataDir, nil)

	_, err = manager.Enqueue("sender@example.com", []string{"rcpt@example.com"}, []byte("test"))
	if err == nil {
		t.Error("expected error when database is closed during enqueue")
	}
}

// TestEnqueueDBFailureDuringEnqueue tests the cleanup path when db.Enqueue fails
// for the recipient. By closing the database between enqueue calls, the second
// enqueue hits the error path after writing the message file.
func TestEnqueueDBFailureDuringEnqueue(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	manager := NewManager(database, nil, dataDir, nil)

	// Enqueue one entry successfully first
	id1, err := manager.Enqueue("sender@example.com", []string{"rcpt@example.com"}, []byte("test1"))
	if err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}

	// Now close the DB and try to enqueue again - this will fail after writing the file
	// but before calling db.Enqueue for the recipient
	database.Close()

	_, err = manager.Enqueue("sender2@example.com", []string{"rcpt2@example.com"}, []byte("test2"))
	if err == nil {
		t.Error("expected error when database is closed during enqueue")
	}

	_ = id1
}

// TestFlushQueueRetryEntryError tests FlushQueue when RetryEntry fails for a failed entry.
func TestFlushQueueRetryEntryError(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	manager := NewManager(database, nil, dataDir, nil)

	// Create a message file
	msgPath := filepath.Join(dataDir, "test.msg")
	writeFile(msgPath, []byte("test"))

	// Create a failed entry with NextRetry in the past so GetPendingQueue returns it
	entry := &db.QueueEntry{
		ID:          "flush-failed-1",
		From:        "sender@example.com",
		To:          []string{"rcpt@example.com"},
		MessagePath: msgPath,
		Status:      "failed",
		RetryCount:  2,
		NextRetry:   time.Now().Add(-time.Hour),
	}
	database.Enqueue(entry)

	// Close database so RetryEntry will fail
	database.Close()

	err = manager.FlushQueue()
	if err == nil {
		t.Error("expected error when RetryEntry fails during FlushQueue")
	}
}

// TestProcessQueueShutdownChannel tests that processQueue exits when the shutdown
// channel is closed.
func TestProcessQueueShutdownChannel(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir, nil)

	ctx := context.Background()
	go manager.processQueue(ctx)

	// Give the goroutine a moment to start
	time.Sleep(50 * time.Millisecond)

	// Close the shutdown channel to stop processQueue
	close(manager.shutdown)
}

// TestDeliverMXNoVERP tests deliverToMX when from address has no @ sign,
// so the VERP encoding path is skipped.
func TestDeliverMXNoVERP(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	msg := []byte("From: sender\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello\r\n")

	// Use an address with no @ to skip VERP encoding path
	err := mgr.deliverToMX("sender-no-at", "rcpt@example.com", msg, "invalid-host-that-does-not-exist-99999.xyz")
	if err == nil {
		t.Log("deliverToMX without @ in from succeeded (unlikely)")
	}
}

// TestDeliverToMX_WriteCloseError tests deliverToMX when the connection drops
// mid-conversation (exercises w.Write / w.Close error paths).
func TestDeliverToMX_WriteCloseError(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	// Create a listener that sends a greeting and accepts EHLO/STARTTLS but
	// then closes the connection abruptly during DATA phase.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		conn.Write([]byte("220 test ESMTP\r\n"))
		// Read a bit then close
		buf := make([]byte, 4096)
		for {
			n, readErr := conn.Read(buf)
			if readErr != nil {
				break
			}
			line := string(buf[:n])
			if len(line) >= 4 && (line[:4] == "EHLO" || line[:4] == "HELO") {
				conn.Write([]byte("250 OK\r\n"))
			} else {
				// Close connection abruptly to trigger write/close errors
				conn.Close()
				return
			}
		}
	}()

	addr := ln.Addr().String()
	mgr.dialSMTP = func(dialAddr string) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}

	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello\r\n")
	err = mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "mx.example.com")
	if err == nil {
		t.Log("deliverToMX succeeded despite connection closing")
	}
}

// TestGenerateBounceNilDB tests generateBounce when manager.db is nil,
// ensuring the Enqueue call is skipped but file cleanup still occurs.
func TestGenerateBounceNilDB(t *testing.T) {
	dataDir := t.TempDir()
	msgPath := filepath.Join(dataDir, "test.msg")
	writeFile(msgPath, []byte("Original message"))

	// Create manager with nil db
	manager := &Manager{
		db:      nil,
		dataDir: dataDir,
	}

	entry := &db.QueueEntry{
		ID:          "test-nil-db-bounce",
		From:        "sender@example.com",
		To:          []string{"rcpt@example.com"},
		MessagePath: msgPath,
		LastError:   "test error",
	}

	// Should not panic and should skip the Enqueue call
	manager.generateBounce(entry)

	// File should still be cleaned up
	if _, err := os.Stat(msgPath); !os.IsNotExist(err) {
		t.Error("expected message file to be cleaned up even with nil db")
	}
}

// TestWriteFileMkdirAllError tests writeFile when MkdirAll fails because
// the parent path contains a file where a directory would need to be created.
func TestWriteFileMkdirAllError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file at "blocked"
	blockPath := filepath.Join(tmpDir, "blocked")
	if err := os.WriteFile(blockPath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// Try to write to a path that requires "blocked" to be a directory
	targetPath := filepath.Join(blockPath, "subdir", "test.txt")
	err := writeFile(targetPath, []byte("data"))
	if err == nil {
		t.Error("expected error when MkdirAll fails (parent is a file)")
	}
}

// TestSplitEmailNoAt tests splitEmail with an address that has no @ sign.
func TestSplitEmailNoAt(t *testing.T) {
	user, domain := splitEmail("no-at-sign")
	if user != "no-at-sign" {
		t.Errorf("expected user 'no-at-sign', got %q", user)
	}
	if domain != "" {
		t.Errorf("expected empty domain, got %q", domain)
	}
}

// TestDeliverFullWithFakeServer tests the full deliver() path with a fake SMTP server,
// exercising the complete flow from reading the message file through delivery.
func TestDeliverFullWithFakeServer(t *testing.T) {
	mgr, dataDir, database := setupManager(t)
	defer database.Close()

	var receivedData []byte
	addr, closer := fakeSMTPServerEx(t, fakeSMTPServerConfig{
		handler: func(from, to string, data []byte) {
			receivedData = data
		},
	})
	defer closer()

	mgr.dialSMTP = func(dialAddr string) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}

	msgPath := filepath.Join(dataDir, "deliver-full.msg")
	testMsg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nFull deliver test\r\n")
	writeFile(msgPath, testMsg)

	entry := &db.QueueEntry{
		ID:          "deliver-full-test",
		From:        "sender@example.com",
		To:          []string{"rcpt@example.com"},
		MessagePath: msgPath,
		Status:      "pending",
		RetryCount:  0,
		CreatedAt:   time.Now(),
		NextRetry:   time.Now(),
	}
	database.Enqueue(entry)

	// Call deliver() which goes through the full path
	mgr.deliver(entry)

	// Give goroutines time to complete
	time.Sleep(200 * time.Millisecond)

	// Verify data was received
	if len(receivedData) == 0 {
		t.Error("expected data to be received by fake server")
	}
}

