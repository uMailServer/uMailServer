package smtp

import (
	"bufio"
	"strconv"
	"strings"
	"testing"
)

// TestHandleBDAT_RejectsSizeThatOverflowsLimit covers the cumulative size guard.
//
// The guard used to compute `int64(s.bdatBuffer.Len() + size)`: both operands are
// `int`, so the sum is formed in `int` and only the (already overflowed) result is
// widened. Once the BDAT buffer holds any bytes, a chunk size near maxInt wraps the
// sum negative, the comparison is false, and the guard is bypassed — the handler
// then reaches make([]byte, size) with an attacker-chosen length and panics.
func TestHandleBDAT_RejectsSizeThatOverflowsLimit(t *testing.T) {
	tests := []struct {
		name string
		// prime is the size of a first, non-LAST chunk written before the
		// oversized one. 0 means the buffer is empty.
		prime int
		// attack is the declared size of the second chunk.
		attack string
	}{
		{name: "overflow with empty buffer", prime: 0, attack: "9223372036854775807"},
		{name: "overflow with non-empty buffer", prime: 1, attack: "9223372036854775807"},
		{name: "overflow just below maxInt", prime: 5, attack: "9223372036854775800"},
		{name: "overflow via int32 boundary", prime: 1, attack: "2147483648"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, clientConn := newBDATTestSession(t)
			defer clientConn.Close()
			reader := bufio.NewReader(clientConn)
			setupBDATSessionState(session)

			// Prime the buffer so bdatBuffer.Len() > 0.
			if tt.prime > 0 {
				if _, err := clientConn.Write(make([]byte, tt.prime)); err != nil {
					t.Fatalf("write prime chunk: %v", err)
				}
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("handleBDAT panicked on a valid prime chunk: %v", r)
						}
					}()
					_ = session.handleBDAT(strconv.Itoa(tt.prime))
				}()
				drainSMTPResponse(t, reader)
			}

			var resp string
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("handleBDAT panicked on an oversized chunk (prime=%d size=%s): %v\n"+
							"the MaxMessageSize guard was bypassed by an integer overflow, so the "+
							"allocation ran with the attacker-chosen length",
							tt.prime, tt.attack, r)
					}
				}()
				_ = session.handleBDAT(tt.attack + " LAST")
				resp = drainSMTPResponse(t, reader)
			}()

			if !strings.HasPrefix(resp, "552") {
				t.Fatalf("oversized BDAT chunk (prime=%d size=%s) was not rejected with 552; got %q",
					tt.prime, tt.attack, strings.TrimSpace(resp))
			}
		})
	}
}

// TestHandleBDAT_AcceptsChunksUpToLimit is the control: ordinary chunking, including
// one that exactly fills MaxMessageSize, must still be accepted. This guards against
// "fixing" the overflow by rejecting valid input.
func TestHandleBDAT_AcceptsChunksUpToLimit(t *testing.T) {
	const limit = 1024 * 1024 // matches newBDATTestSession's MaxMessageSize

	session, clientConn := newBDATTestSession(t)
	defer clientConn.Close()
	reader := bufio.NewReader(clientConn)
	setupBDATSessionState(session)

	chunks := []int{10, 10, limit - 20} // sums to exactly limit
	for i, n := range chunks {
		if _, err := clientConn.Write(make([]byte, n)); err != nil {
			t.Fatalf("write chunk %d: %v", i, err)
		}
		last := ""
		if i == len(chunks)-1 {
			last = " LAST"
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("handleBDAT panicked on a valid in-limit chunk (%d bytes): %v", n, r)
				}
			}()
			_ = session.handleBDAT(strconv.Itoa(n) + last)
		}()
		resp := drainSMTPResponse(t, reader)
		if last == "" {
			if !strings.HasPrefix(resp, "250") {
				t.Fatalf("non-final chunk %d rejected: %q", n, strings.TrimSpace(resp))
			}
			continue
		}
		if !strings.HasPrefix(resp, "250") {
			t.Fatalf("final chunk totalling exactly MaxMessageSize (%d) was rejected: %q",
				limit, strings.TrimSpace(resp))
		}
	}
}

// TestHandleBDAT_RejectsCumulativeOverflow confirms the guard still tracks the
// running total: a final chunk that pushes the buffer past MaxMessageSize is refused
// even when no single chunk is oversized.
func TestHandleBDAT_RejectsCumulativeOverflow(t *testing.T) {
	const limit = 1024 * 1024

	session, clientConn := newBDATTestSession(t)
	defer clientConn.Close()
	reader := bufio.NewReader(clientConn)
	setupBDATSessionState(session)

	if _, err := clientConn.Write(make([]byte, limit)); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	_ = session.handleBDAT(strconv.Itoa(limit))
	drainSMTPResponse(t, reader)

	if _, err := clientConn.Write(make([]byte, 1)); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}
	_ = session.handleBDAT("1 LAST")
	resp := drainSMTPResponse(t, reader)

	if !strings.HasPrefix(resp, "552") {
		t.Fatalf("chunk exceeding the cumulative limit was not rejected with 552; got %q",
			strings.TrimSpace(resp))
	}
}

// drainSMTPResponse reads one response line from the client side of the session.
func drainSMTPResponse(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read SMTP response: %v", err)
	}
	return line
}
