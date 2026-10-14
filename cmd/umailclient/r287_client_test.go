package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/mail"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Re-exec helper: run main() with the args in R287_ARGS.
func init() {
	if os.Getenv("R287_HELPER") == "1" {
		os.Args = append([]string{"umailclient"}, strings.Fields(os.Getenv("R287_ARGS"))...)
		main()
		os.Exit(0)
	}
}

func r287RunMain(t *testing.T, args string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "R287_HELPER=1", "R287_ARGS="+args)
	out, _ := cmd.CombinedOutput()
	return cmd.ProcessState.ExitCode(), string(out)
}

// r287Server is a loopback SMTP server answering the end of DATA with
// dataReply; the first DATA payload is delivered on the returned channel.
func r287Server(t *testing.T, dataReply string) (int, chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan string, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				fmt.Fprint(c, "220 fake\r\n")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					u := strings.ToUpper(strings.TrimSpace(l))
					switch {
					case strings.HasPrefix(u, "DATA"):
						fmt.Fprint(c, "354 go\r\n")
						var sb strings.Builder
						for {
							d, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if d == ".\r\n" {
								break
							}
							sb.WriteString(d)
						}
						got <- sb.String()
						fmt.Fprint(c, dataReply+"\r\n")
					case strings.HasPrefix(u, "QUIT"):
						fmt.Fprint(c, "221 bye\r\n")
						return
					default:
						fmt.Fprint(c, "250 ok\r\n")
					}
				}
			}(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, got
}

func TestR287SendReportsDataRejection(t *testing.T) {
	for _, tc := range []struct {
		reply   string
		wantErr bool
	}{
		{"250 queued", false},
		{"554 5.7.1 rejected", true},
		{"452 4.3.1 insufficient storage", true},
	} {
		port, _ := r287Server(t, tc.reply)
		err := NewSMTPClient("127.0.0.1", port, "", "", false, false).Send("a@x.test", "b@x.test", "s", "b")
		if (err != nil) != tc.wantErr {
			t.Errorf("reply %q: err=%v wantErr=%v", tc.reply, err, tc.wantErr)
		}
	}
}

func TestR287MDNHasRealHeader(t *testing.T) {
	port, got := r287Server(t, "250 queued")
	code, out := r287RunMain(t, fmt.Sprintf("mdn --host 127.0.0.1 --port %d --to b@x.test --subject Hello", port))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	m, err := mail.ReadMessage(strings.NewReader(<-got))
	if err != nil {
		t.Fatal(err)
	}
	if v := m.Header.Get("Disposition-Notification-To"); v != "admin@localhost" {
		t.Errorf("Disposition-Notification-To = %q", v)
	}
	if len(m.Header["From"]) != 1 || m.Header.Get("Subject") != "Hello" {
		t.Errorf("headers duplicated or wrong: From=%v Subject=%q", m.Header["From"], m.Header.Get("Subject"))
	}
}

func TestR287LoadArgsValidated(t *testing.T) {
	if code, out := r287RunMain(t, "bench --host 127.0.0.1 --port 1 --count 0 --concurrency 1"); code != 0 {
		t.Fatalf("control: exit %d %s", code, out)
	}
	for _, a := range []string{
		"bench --host 127.0.0.1 --port 1 --count 1 --concurrency 0",
		"bench --host 127.0.0.1 --port 1 --count -1 --concurrency 1",
		"stress --host 127.0.0.1 --port 1 --count 1 --concurrency 0",
		"stress --host 127.0.0.1 --port 1 --count -1 --concurrency 5",
	} {
		if code, out := r287RunMain(t, a); code != 1 {
			t.Errorf("%q: exit %d, want 1: %.100s", a, code, out)
		}
	}
}
