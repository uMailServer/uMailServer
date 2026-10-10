package pop3

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// FuzzPOP3Session feeds arbitrary command lines through a full session and
// requires that the session neither panics nor hangs.
func FuzzPOP3Session(f *testing.F) {
	for _, s := range []string{
		"USER alice\r\nPASS pw\r\nSTAT\r\nLIST\r\nRETR 1\r\nTOP 1 1\r\nDELE 1\r\nRSET\r\nUIDL\r\nNOOP\r\nQUIT\r\n",
		"TOP 1 -1\r\nRETR 99999999999999999999\r\nLIST 0\r\n",
		"USER\r\nPASS\r\n\r\n\x00\xff\r\nAPOP a b\r\nAUTH\r\nCAPA\r\nSTLS\r\nQUIT\r\n",
		"USER alice\r\nPASS pw\r\nTOP 1 99999999999\r\nTOP x y\r\nDELE 0\r\nDELE -1\r\nQUIT\r\n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			t.Skip()
		}
		srv, _ := newUpdateServer(t)
		c1, c2 := net.Pipe()
		session := NewSession(c1, srv)
		done := make(chan struct{})
		go func() {
			defer close(done)
			session.Handle()
			c1.Close()
		}()
		go func() {
			rd := bufio.NewReader(c2)
			for {
				if _, err := rd.ReadString('\n'); err != nil {
					return
				}
			}
		}()
		c2.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = c2.Write([]byte(strings.ReplaceAll(input, "\x00", "") + "\r\nQUIT\r\n"))
		c2.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("session hung on %q", input)
		}
	})
}
