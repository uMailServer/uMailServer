package cli

// Regression tests for extractIMAPMessageData (internal/cli/migrate.go).
//
// Defect history: extractIMAPMessageData filled its output buffer with a
// single body.Read call. io.Reader makes no guarantee that one Read fills the
// buffer — network section readers can return short reads on large messages —
// so the tail of the returned slice stayed zero-filled and the TRUNCATED
// content was stored as the migrated message (silent data corruption under
// the content-hash message ID). Fix: io.ReadFull, which loops until the
// buffer is full or the source errors.
//
// Contract basis: io.Reader semantics + the function's purpose ("extracts the
// raw message data"): the returned bytes must equal the full source content.

import (
	"bytes"
	"io"
	"testing"

	extimap "github.com/emersion/go-imap"
)

// shortReadReader returns at most 1 byte per Read (worst-case TCP
// segmentation) while Len() reports the full remaining length, mirroring a
// network-backed section reader.
type shortReadReader struct {
	data []byte
	pos  int
}

func (s *shortReadReader) Len() int { return len(s.data) - s.pos }

func (s *shortReadReader) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	n := 1
	if len(p) < n {
		n = len(p)
	}
	copied := copy(p[:n], s.data[s.pos:])
	s.pos += copied
	return copied, nil
}

// fullReadReader mimics bytes.Reader semantics (Read fills the buffer).
type fullReadReader struct {
	data []byte
	pos  int
}

func (f *fullReadReader) Len() int { return len(f.data) - f.pos }

func (f *fullReadReader) Read(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func msgWithBody(r extimap.Literal) *extimap.Message {
	return &extimap.Message{
		SeqNum: 1,
		Body: map[*extimap.BodySectionName]extimap.Literal{
			&extimap.BodySectionName{}: r,
		},
	}
}

// TestExtractIMAPMessageData_HandlesShortReads pins the io.ReadFull contract:
// a body reader that returns one byte per Read must still produce the FULL
// message content. Pre-fix, the tail of the returned slice was zero-filled.
func TestExtractIMAPMessageData_HandlesShortReads(t *testing.T) {
	content := []byte("From: alice@example.com\r\n\r\nmigrated body 0123456789 ABCDEFGHIJ")

	msg := msgWithBody(&shortReadReader{data: content})
	got, err := extractIMAPMessageData(msg)
	if err != nil {
		t.Fatalf("extractIMAPMessageData returned error: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("short-read body truncated: got %d bytes, want %d; mismatch at first differing byte %d",
			len(got), len(content), firstDiff(got, content))
	}
}

// TestExtractIMAPMessageData_FullReadBodyControl is the control case: a
// reader that fills buffers in one call must keep working (both sides).
func TestExtractIMAPMessageData_FullReadBodyControl(t *testing.T) {
	content := []byte("From: bob@example.com\r\n\r\ncontrol body")

	msg := msgWithBody(&fullReadReader{data: content})
	got, err := extractIMAPMessageData(msg)
	if err != nil {
		t.Fatalf("extractIMAPMessageData returned error: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("full-read body corrupted: got %q, want %q", got, content)
	}
}

// TestExtractIMAPMessageData_NoBodyErrors pins the no-body error path.
func TestExtractIMAPMessageData_NoBodyErrors(t *testing.T) {
	msg := &extimap.Message{SeqNum: 1, Body: map[*extimap.BodySectionName]extimap.Literal{}}
	if _, err := extractIMAPMessageData(msg); err == nil {
		t.Fatal("expected error for message without body section, got nil")
	}
}

func firstDiff(got, want []byte) int {
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			return i
		}
	}
	return -1
}
