//go:build !race

package server

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/av"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/imap"
)

// ---------------------------------------------------------------------------
// getUserSecret tests
// ---------------------------------------------------------------------------

func TestGetUserSecret_ActiveUser(t *testing.T) {
	srv := helperServer(t)
	hashedPassword := "$2a$10$BXVavbSB/53WBHDuJlzIHeCsgSTgzrOqtbdPmrkPa68dA3jYmKux2"
	account := &db.AccountData{
		Email:        "secretuser@test.example.com",
		LocalPart:    "secretuser",
		Domain:       "test.example.com",
		PasswordHash: hashedPassword,
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := srv.database.CreateAccount(account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}
	secret, err := srv.getUserSecret("secretuser@test.example.com")
	if err != nil {
		t.Fatalf("getUserSecret returned error: %v", err)
	}
	if secret != hashedPassword {
		t.Errorf("expected hash %q, got %q", hashedPassword, secret)
	}
}

func TestGetUserSecret_InactiveUser(t *testing.T) {
	srv := helperServer(t)
	account := &db.AccountData{
		Email:        "inactive@test.example.com",
		LocalPart:    "inactive",
		Domain:       "test.example.com",
		PasswordHash: "somehash",
		IsActive:     false,
		CreatedAt:    time.Now(),
	}
	if err := srv.database.CreateAccount(account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}
	_, err := srv.getUserSecret("inactive@test.example.com")
	if err == nil {
		t.Fatal("expected error for inactive user")
	}
}

func TestGetUserSecret_NonexistentUser(t *testing.T) {
	srv := helperServer(t)
	_, err := srv.getUserSecret("nobody@test.example.com")
	if err == nil {
		t.Fatal("expected error for nonexistent user")
	}
}

func TestGetUserSecret_UserWithoutDomain(t *testing.T) {
	srv := helperServer(t)
	_, err := srv.getUserSecret("justauser")
	if err == nil {
		t.Fatal("expected error for user without domain")
	}
}

// ---------------------------------------------------------------------------
// avScannerAdapter tests
// ---------------------------------------------------------------------------

func TestAVScannerAdapter_IsEnabled_True(t *testing.T) {
	scanner := av.NewScanner(av.Config{Enabled: true, Addr: "127.0.0.1:3310"})
	adapter := &avScannerAdapter{inner: scanner}
	if !adapter.IsEnabled() {
		t.Error("expected IsEnabled=true when scanner is enabled with addr")
	}
}

func TestAVScannerAdapter_IsEnabled_NoAddr(t *testing.T) {
	scanner := av.NewScanner(av.Config{Enabled: true, Addr: ""})
	adapter := &avScannerAdapter{inner: scanner}
	if adapter.IsEnabled() {
		t.Error("expected IsEnabled=false when addr is empty")
	}
}
func TestAVScannerAdapter_IsEnabled_Disabled(t *testing.T) {
	scanner := av.NewScanner(av.Config{Enabled: false, Addr: "127.0.0.1:3310"})
	adapter := &avScannerAdapter{inner: scanner}
	if adapter.IsEnabled() {
		t.Error("expected IsEnabled=false when scanner is disabled")
	}
}
func TestAVScannerAdapter_Scan_DisabledScanner(t *testing.T) {
	scanner := av.NewScanner(av.Config{Enabled: false, Addr: ""})
	adapter := &avScannerAdapter{inner: scanner}
	result, err := adapter.Scan([]byte("data"))
	if err != nil {
		t.Fatalf("Scan on disabled scanner should not error, got: %v", err)
	}
	if result.Infected {
		t.Error("expected clean result for disabled scanner")
	}
}
func TestAVScannerAdapter_Scan_Clean(t *testing.T) {
	ln := startFakeClamAVServer(t, false)
	defer ln.Close()
	scanner := av.NewScanner(av.Config{Enabled: true, Addr: ln.Addr().String()})
	adapter := &avScannerAdapter{inner: scanner}
	result, err := adapter.Scan([]byte("clean data"))
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if result.Infected {
		t.Error("expected clean result")
	}
}
func TestAVScannerAdapter_Scan_Infected(t *testing.T) {
	ln := startFakeClamAVServer(t, true)
	defer ln.Close()
	scanner := av.NewScanner(av.Config{Enabled: true, Addr: ln.Addr().String()})
	adapter := &avScannerAdapter{inner: scanner}
	result, err := adapter.Scan([]byte("infected data"))
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !result.Infected {
		t.Fatal("expected infected result")
	}
	if result.Virus == "" {
		t.Error("expected virus name in result")
	}
}
func TestAVScannerAdapter_Scan_ConnectionError(t *testing.T) {
	scanner := av.NewScanner(av.Config{Enabled: true, Addr: "127.0.0.1:1"})
	adapter := &avScannerAdapter{inner: scanner}
	_, err := adapter.Scan([]byte("data"))
	if err == nil {
		t.Error("expected error when scanner cannot connect")
	}
}

// ---------------------------------------------------------------------------
// pop3MailstoreAdapter tests
// ---------------------------------------------------------------------------

func newTestPop3Adapter(t *testing.T) (*pop3MailstoreAdapter, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	mailstore, err := imap.NewBboltMailstore(tmpDir + "/mail")
	if err != nil {
		t.Fatalf("failed to create BboltMailstore: %v", err)
	}
	adapter := &pop3MailstoreAdapter{
		mailstore: mailstore,
	}
	return adapter, func() { mailstore.Close() }
}
func TestPop3Adapter_Authenticate(t *testing.T) {
	adapter, cleanup := newTestPop3Adapter(t)
	defer cleanup()
	// Without authFunc injected, Authenticate delegates to storage.Database.AuthenticateUser
	// which is not implemented — expect failure
	_, err := adapter.Authenticate("user@test.com", "password")
	if err == nil {
		t.Error("expected error from unimplemented storage auth")
	}
}
func TestPop3Adapter_ListMessages_Empty(t *testing.T) {
	adapter, cleanup := newTestPop3Adapter(t)
	defer cleanup()
	msgs, err := adapter.ListMessages("testuser")
	if err != nil {
		t.Fatalf("ListMessages returned error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for empty mailbox, got %d", len(msgs))
	}
}
func TestPop3Adapter_GetMessage_NotFound(t *testing.T) {
	adapter, cleanup := newTestPop3Adapter(t)
	defer cleanup()
	_, err := adapter.GetMessage("testuser", 0)
	if err == nil {
		t.Fatal("expected error when message not found")
	}
}
func TestPop3Adapter_GetMessageData_NotFound(t *testing.T) {
	adapter, cleanup := newTestPop3Adapter(t)
	defer cleanup()
	_, err := adapter.GetMessageData("testuser", 0)
	if err == nil {
		t.Fatal("expected error when message data not found")
	}
}
func TestPop3Adapter_DeleteMessage_Empty(t *testing.T) {
	adapter, cleanup := newTestPop3Adapter(t)
	defer cleanup()
	err := adapter.DeleteMessage("testuser", 0)
	t.Logf("DeleteMessage on empty: %v", err)
}
func TestPop3Adapter_GetMessageCount_Empty(t *testing.T) {
	adapter, cleanup := newTestPop3Adapter(t)
	defer cleanup()
	count, err := adapter.GetMessageCount("testuser")
	if err != nil {
		t.Fatalf("GetMessageCount returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 messages, got %d", count)
	}
}
func TestPop3Adapter_GetMessageSize_NotFound(t *testing.T) {
	adapter, cleanup := newTestPop3Adapter(t)
	defer cleanup()
	_, err := adapter.GetMessageSize("testuser", 0)
	if err == nil {
		t.Fatal("expected error when message not found")
	}
}
func TestPop3Adapter_WithMessages(t *testing.T) {
	adapter, cleanup := newTestPop3Adapter(t)
	defer cleanup()
	msgData := []byte("Subject: Test\r\nFrom: sender@test.com\r\n\r\nHello World")
	for i := 0; i < 3; i++ {
		err := adapter.mailstore.AppendMessage("testuser", "INBOX", nil, time.Now(), msgData)
		if err != nil {
			t.Fatalf("AppendMessage %d failed: %v", i, err)
		}
	}
	msgs, err := adapter.ListMessages("testuser")
	if err != nil {
		t.Fatalf("ListMessages returned error: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	count, err := adapter.GetMessageCount("testuser")
	if err != nil {
		t.Fatalf("GetMessageCount returned error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count=3, got %d", count)
	}
	for i := 0; i < 3; i++ {
		msg, err := adapter.GetMessage("testuser", i)
		if err != nil {
			t.Errorf("GetMessage(%d) returned error: %v", i, err)
			continue
		}
		if msg.Index != i {
			t.Errorf("GetMessage(%d) returned Index=%d", i, msg.Index)
		}
	}
	data, err := adapter.GetMessageData("testuser", 0)
	if err != nil {
		t.Fatalf("GetMessageData returned error: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty message data")
	}
	size, err := adapter.GetMessageSize("testuser", 0)
	if err != nil {
		t.Fatalf("GetMessageSize returned error: %v", err)
	}
	if size <= 0 {
		t.Errorf("expected positive size, got %d", size)
	}
	err = adapter.DeleteMessage("testuser", 0)
	if err != nil {
		t.Logf("DeleteMessage returned error: %v", err)
	}
}
func TestPop3Adapter_GetMessage_OutOfBounds(t *testing.T) {
	adapter, cleanup := newTestPop3Adapter(t)
	defer cleanup()
	msgData := []byte("Subject: One\r\n\r\nBody")
	err := adapter.mailstore.AppendMessage("testuser", "INBOX", nil, time.Now(), msgData)
	if err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}
	_, err = adapter.GetMessage("testuser", 99)
	if err == nil {
		t.Error("expected error for out-of-bounds message index")
	}
}
func TestPop3Adapter_GetMessageData_OutOfBounds(t *testing.T) {
	adapter, cleanup := newTestPop3Adapter(t)
	defer cleanup()
	_, err := adapter.GetMessageData("testuser", 99)
	if err == nil {
		t.Error("expected error for out-of-bounds message data")
	}
}
func TestPop3Adapter_GetMessageSize_OutOfBounds(t *testing.T) {
	adapter, cleanup := newTestPop3Adapter(t)
	defer cleanup()
	_, err := adapter.GetMessageSize("testuser", 99)
	if err == nil {
		t.Error("expected error for out-of-bounds message size")
	}
}

// ---------------------------------------------------------------------------
// Fake ClamAV server helper
// ---------------------------------------------------------------------------
func startFakeClamAVServer(t *testing.T, returnInfected bool) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				// Read INSTREAM command (null-terminated: "zINSTREAM\0")
				_, err := reader.ReadBytes(0)
				if err != nil {
					return
				}
				// Read chunks: 4-byte length prefix + data, until 0-length
				for {
					header := make([]byte, 4)
					if _, err := io.ReadFull(reader, header); err != nil {
						return
					}
					chunkLen := binary.BigEndian.Uint32(header)
					if chunkLen == 0 {
						break
					}
					chunk := make([]byte, chunkLen)
					if _, err := io.ReadFull(reader, chunk); err != nil {
						return
					}
				}
				// Write response
				if returnInfected {
					fmt.Fprintf(conn, "stream: Eicar-Test-Signature FOUND\n")
				} else {
					fmt.Fprintf(conn, "stream: OK\n")
				}
			}()
		}
	}()
	return ln
}
