package av

// Regression tests for finding F5107: only "stream: OK" is a clean verdict; any other clamd reply is an error.

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func v5107Scanner(reply string) *Scanner {
	s := NewScanner(Config{Enabled: true, Addr: "clamd.test:3310", Timeout: 5 * time.Second})
	s.dial = func(_, _ string, _ time.Duration) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			cmd := make([]byte, len("zINSTREAM\x00"))
			if _, err := io.ReadFull(server, cmd); err != nil {
				return
			}
			for {
				var hdr [4]byte
				if _, err := io.ReadFull(server, hdr[:]); err != nil {
					return
				}
				n := binary.BigEndian.Uint32(hdr[:])
				if n == 0 {
					break
				}
				if _, err := io.CopyN(io.Discard, server, int64(n)); err != nil {
					return
				}
			}
			_, _ = server.Write([]byte(reply))
		}()
		return client, nil
	}
	return s
}

func TestF5107Reproduction(t *testing.T) {
	for _, r := range []string{"UNKNOWN COMMAND\x00", "COMMAND READ TIMED OUT\x00", "220 mx.example.test ESMTP\x00"} {
		if res, err := v5107Scanner(r).Scan([]byte("x")); err == nil {
			t.Fatalf("%q reported as %+v", r, res)
		}
	}
}

func TestF5107Edges(t *testing.T) {
	// Clean with newline framing and multi-chunk data.
	big := make([]byte, 100000)
	if res, err := v5107Scanner("stream: OK\n").Scan(big); err != nil || res.Infected {
		t.Fatalf("clean: %+v %v", res, err)
	}
	// Infected verdict still parsed.
	if res, err := v5107Scanner("stream: Win.Test.EICAR_HDB-1 FOUND\x00").Scan(nil); err != nil || !res.Infected || res.Virus != "Win.Test.EICAR_HDB-1" {
		t.Fatalf("found: %+v %v", res, err)
	}
	// Size-limit ERROR and empty reply are errors.
	for _, r := range []string{"INSTREAM size limit exceeded. ERROR\x00", "\x00"} {
		if _, err := v5107Scanner(r).Scan([]byte("x")); err == nil {
			t.Fatalf("%q not an error", r)
		}
	}
}
