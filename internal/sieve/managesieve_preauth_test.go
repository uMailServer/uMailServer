package sieve

// Regression tests for audit finding F5037 (see .temp_files/ledger_internal_sieve.md, Round 22).

import (
	"bufio"
	"bytes"
	"net"
	"strings"
	"testing"
	"time"
)

// regF5037Exchange runs the real handleConn over net.Pipe and sends the
// given bytes without authenticating. It returns the first response line.
func regF5037Exchange(t *testing.T, payload string) string {
	t.Helper()
	srv := NewManageSieveServer(NewManager(), nil)
	client, server := net.Pipe()
	srv.wg.Add(1)
	go srv.handleConn(server)
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(client)
	if line, err := r.ReadString('\n'); err != nil || !strings.HasPrefix(line, "OK") {
		t.Fatalf("INVALID greeting %q %v", line, err)
	}
	go func() { _, _ = client.Write([]byte(payload)) }()
	line, err := r.ReadString('\n')
	if err != nil {
		return "ERR:" + err.Error()
	}
	return strings.TrimSpace(line)
}

func TestF5037_Control(t *testing.T) {
	got := regF5037Exchange(t, "NOOP\r\n")
	t.Logf("CONTROL EXPECTED: NOOP OK pre-auth | ACTUAL: %q\n", got)
	if !strings.HasPrefix(got, "OK") {
		t.Fatal("INVALID control")
	}
}

func TestF5037_Regression(t *testing.T) {
	got := regF5037Exchange(t, "CHECKSCRIPT 5\r\nkeep;")
	t.Logf("EXPECTED: CHECKSCRIPT refused before AUTHENTICATE (NO) | ACTUAL: %q\n", got)
	if strings.HasPrefix(got, "OK") {
		t.Fatal("DEFECT F5037: CHECKSCRIPT is executed before authentication (RFC 5804 §2.12)")
	}
}

func TestF5037_Edges(t *testing.T) {
	// Every script-handling command must be refused pre-auth.
	for _, p := range []string{"CHECKSCRIPT 5\r\nkeep;", "PUTSCRIPT \"x\" 5\r\nkeep;", "LISTSCRIPTS\r\n", "GETSCRIPT \"x\"\r\n", "SETACTIVE \"x\"\r\n", "DELETESCRIPT \"x\"\r\n"} {
		got := regF5037Exchange(t, p)
		t.Logf("EDGE pre-auth %q | ACTUAL %q\n", strings.SplitN(p, "\r\n", 2)[0], got)
		if strings.HasPrefix(got, "OK") {
			t.Fatalf("edge %q accepted pre-auth", p)
		}
	}
	// After authentication CHECKSCRIPT still works.
	mgr := NewManager()
	srv := NewManageSieveServer(mgr, nil)
	conn := &mockConn{readBuf: bytes.NewBufferString("keep;"), writeBuf: new(bytes.Buffer)}
	sess := &manageSieveSession{conn: conn, reader: &manageSieveReader{r: conn}, user: "u", manager: mgr}
	err := srv.cmdCheckScript(sess, []string{"5"})
	t.Logf("EDGE authenticated CHECKSCRIPT | ACTUAL err=%v out=%q\n", err, conn.writeBuf.String())
	if err != nil || !strings.HasPrefix(conn.writeBuf.String(), "OK") {
		t.Fatal("edge authenticated CHECKSCRIPT failed")
	}
}
