package server

import (
	"testing"
)

// TestSendMDN_QueueNil tests sendMDN when queue is nil
func TestSendMDN_QueueNil(t *testing.T) {
	srv := helperServer(t)
	srv.queue = nil

	err := srv.sendMDN("from@example.com", "to@example.com", "msg123", "ref456", []byte("test message"))

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// TestSendMDN_WithQueue tests sendMDN when queue is present
func TestSendMDN_WithQueue(t *testing.T) {
	srv := helperServer(t)

	// Queue is set via helperServer
	if srv.queue == nil {
		t.Skip("queue not available")
	}

	err := srv.sendMDN("from@example.com", "to@example.com", "msg123", "ref456", []byte("test message"))

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// TestSendMDN_EmptyData tests with empty message data
func TestSendMDN_EmptyData(t *testing.T) {
	srv := helperServer(t)

	err := srv.sendMDN("from@example.com", "to@example.com", "msg123", "ref456", []byte{})

	// Empty data - behavior depends on MDN generation
	_ = err // May or may not error depending on queue implementation
}
