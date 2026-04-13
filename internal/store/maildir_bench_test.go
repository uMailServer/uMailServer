package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkMaildirDeliver measures message delivery performance
func BenchmarkMaildirDeliver(b *testing.B) {
	tempDir := b.TempDir()
	store := NewMaildirStore(tempDir)

	msg := []byte("From: sender@test.com\r\n" +
		"To: recipient@test.com\r\n" +
		"Subject: Test Subject\r\n" +
		"\r\n" +
		"This is the message body.")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := store.Deliver("example.com", "user", "INBOX", msg)
		if err != nil {
			b.Fatalf("deliver failed: %v", err)
		}
	}
}

// BenchmarkMaildirDeliverWithFlags measures delivery with flag setting
func BenchmarkMaildirDeliverWithFlags(b *testing.B) {
	tempDir := b.TempDir()
	store := NewMaildirStore(tempDir)

	msg := []byte("From: sender@test.com\r\n" +
		"To: recipient@test.com\r\n" +
		"Subject: Test Subject\r\n" +
		"\r\n" +
		"This is the message body.")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := store.DeliverWithFlags("example.com", "user", "INBOX", msg, "S")
		if err != nil {
			b.Fatalf("deliver with flags failed: %v", err)
		}
	}
}

// BenchmarkMaildirFetch measures message retrieval performance
func BenchmarkMaildirFetch(b *testing.B) {
	tempDir := b.TempDir()
	store := NewMaildirStore(tempDir)

	msg := []byte("From: sender@test.com\r\n" +
		"To: recipient@test.com\r\n" +
		"Subject: Test Subject\r\n" +
		"\r\n" +
		"This is the message body.")

	// Pre-deliver a message
	filename, err := store.Deliver("example.com", "user", "INBOX", msg)
	if err != nil {
		b.Fatalf("setup deliver failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := store.Fetch("example.com", "user", "INBOX", filename)
		if err != nil {
			b.Fatalf("fetch failed: %v", err)
		}
	}
}

// BenchmarkMaildirList measures mailbox listing performance
func BenchmarkMaildirList(b *testing.B) {
	tempDir := b.TempDir()
	store := NewMaildirStore(tempDir)

	msg := []byte("From: sender@test.com\r\n" +
		"To: recipient@test.com\r\n" +
		"Subject: Test\r\n" +
		"\r\n" +
		"Body")

	// Pre-deliver multiple messages
	for i := 0; i < 100; i++ {
		_, err := store.Deliver("example.com", "user", "INBOX", msg)
		if err != nil {
			b.Fatalf("setup deliver failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := store.List("example.com", "user", "INBOX")
		if err != nil {
			b.Fatalf("list failed: %v", err)
		}
	}
}

// BenchmarkMaildirListLargeMailbox measures listing with many messages
func BenchmarkMaildirListLargeMailbox(b *testing.B) {
	tempDir := b.TempDir()
	store := NewMaildirStore(tempDir)

	msg := []byte("From: sender@test.com\r\n" +
		"To: recipient@test.com\r\n" +
		"Subject: Test\r\n" +
		"\r\n" +
		"Body")

	// Pre-deliver many messages
	for i := 0; i < 1000; i++ {
		_, err := store.Deliver("example.com", "user", "INBOX", msg)
		if err != nil {
			b.Fatalf("setup deliver failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := store.List("example.com", "user", "INBOX")
		if err != nil {
			b.Fatalf("list failed: %v", err)
		}
	}
}

// BenchmarkMaildirLargeMessage measures delivery of large messages
func BenchmarkMaildirDeliverLargeMessage(b *testing.B) {
	tempDir := b.TempDir()
	store := NewMaildirStore(tempDir)

	// Create a 1MB message
	body := make([]byte, 1024*1024)
	for i := range body {
		body[i] = byte('a' + (i % 26))
	}
	msg := fmt.Appendf(nil, "From: sender@test.com\r\n"+
		"To: recipient@test.com\r\n"+
		"Subject: Large Message\r\n"+
		"\r\n"+
		"%s", body)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := store.Deliver("example.com", "user", "INBOX", msg)
		if err != nil {
			b.Fatalf("deliver failed: %v", err)
		}
	}
}

// BenchmarkMaildirMove measures message move performance
func BenchmarkMaildirMove(b *testing.B) {
	tempDir := b.TempDir()
	store := NewMaildirStore(tempDir)

	msg := []byte("From: sender@test.com\r\n" +
		"To: recipient@test.com\r\n" +
		"Subject: Test\r\n" +
		"\r\n" +
		"Body")

	// Create Sent folder
	sentDir := filepath.Join(tempDir, "domains", "example.com", "users", "user", "Maildir", ".Sent")
	os.MkdirAll(filepath.Join(sentDir, "cur"), 0o755)
	os.MkdirAll(filepath.Join(sentDir, "new"), 0o755)
	os.MkdirAll(filepath.Join(sentDir, "tmp"), 0o755)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Deliver a fresh message for each iteration
		filename, err := store.Deliver("example.com", "user", "INBOX", msg)
		if err != nil {
			b.Fatalf("setup deliver failed: %v", err)
		}
		b.StartTimer()

		err = store.Move("example.com", "user", "INBOX", "Sent", filename)
		if err != nil {
			b.Fatalf("move failed: %v", err)
		}
	}
}
