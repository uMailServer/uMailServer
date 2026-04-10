package imap

import (
	"testing"
)

// --- handleSort tests ---

func TestHandleSort_NoMailbox(t *testing.T) {
	t.Skip("handleSort requires full session with mailstore and selected mailbox")
}

func TestHandleSort_WithCharset(t *testing.T) {
	t.Skip("handleSort requires full session with mailstore")
}

// --- handleThread tests ---

func TestHandleThread_NoMailbox(t *testing.T) {
	t.Skip("handleThread requires full session with mailstore")
}

func TestHandleThread_OrderedSubject(t *testing.T) {
	t.Skip("handleThread requires full session with mailstore")
}

// --- handleUIDSort tests ---

func TestHandleUIDSort(t *testing.T) {
	t.Skip("handleUIDSort requires full session with mailstore")
}

// --- handleUIDThread tests ---

func TestHandleUIDThread(t *testing.T) {
	t.Skip("handleUIDThread requires full session with mailstore")
}
