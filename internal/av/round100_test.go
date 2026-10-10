package av

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestF5824_DisabledReportsSkipped(t *testing.T) {
	r, err := NewScanner(Config{}).Scan([]byte("x"))
	if err != nil || r.Infected || !r.Skipped {
		t.Fatalf("got %+v %v", r, err)
	}
}

// clamd that replies with a size-limit error as soon as it has read a bit,
// then closes: the reason must surface in the error.
func TestF5824_SizeLimitReplySurfaces(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		br := bufio.NewReader(c)
		_, _ = br.ReadString(0)
		buf := make([]byte, 4+32768)
		_, _ = io.ReadFull(br, buf)
		_, _ = c.Write([]byte("INSTREAM size limit exceeded. ERROR\x00"))
		c.Close()
	}()
	s := NewScanner(Config{Enabled: true, Addr: ln.Addr().String(), Timeout: 5 * time.Second})
	_, err = s.Scan(make([]byte, 64<<20))
	if err == nil || !strings.Contains(err.Error(), "size limit exceeded") {
		t.Fatalf("want size-limit reason in error, got %v", err)
	}
}
