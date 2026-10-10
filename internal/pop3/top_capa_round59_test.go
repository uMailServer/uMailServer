package pop3

// Regression tests for TOP line counting and CAPA STLS advertisement.
//
// F5400: TOP n with n larger than the body split the body on "\n" and sent
// the empty string after the final CRLF as an extra body line (RFC 1939 §7).
// F5403: CAPA advertised STLS even when the certificate could not be loaded,
// so STLS then refused it.

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type topStore struct{ data []byte }

func (m *topStore) Authenticate(_, p string) (bool, error) { return p == "pw", nil }
func (m *topStore) ListMessages(string) ([]*Message, error) {
	return []*Message{{Index: 1, UID: "u1", Size: int64(len(m.data))}}, nil
}
func (m *topStore) GetMessage(string, int) (*Message, error) { return nil, io.EOF }
func (m *topStore) GetMessageData(string, int) ([]byte, error) {
	return append([]byte(nil), m.data...), nil
}
func (m *topStore) DeleteMessage(string, int) error           { return nil }
func (m *topStore) GetMessageCount(string) (int, error)       { return 1, nil }
func (m *topStore) GetMessageSize(string, int) (int64, error) { return 0, nil }

func (c *admClient) multi() []string {
	out := []string{}
	for {
		l := c.line()
		if l == "." || l == "<closed>" {
			return out
		}
		out = append(out, l)
	}
}

func TestTOP_LineCountBeyondBody(t *testing.T) {
	cases := []struct {
		data, cmd string
		want      []string
	}{
		{"Subject: x\r\n\r\nB1\r\nB2\r\n", "TOP 1 10", []string{"Subject: x", "", "B1", "B2"}},
		{"Subject: x\r\n\r\nB1\r\nB2\r\n", "TOP 1 1", []string{"Subject: x", "", "B1"}},
		{"Subject: x\r\n\r\n", "TOP 1 5", []string{"Subject: x", ""}},
		{"Subject: x\r\n\r\nB1\r\n\r\n", "TOP 1 9", []string{"Subject: x", "", "B1", ""}},
		{"Subject: x\n\nB1\nB2\n", "TOP 1 9", []string{"Subject: x", "", "B1", "B2"}},
	}
	for _, tc := range cases {
		srv := NewServer("127.0.0.1:0", &topStore{data: []byte(tc.data)}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		cl, _ := admConn(t, srv, nil)
		cl.line()
		for _, l := range []string{"USER u", "PASS pw", tc.cmd} {
			if r := cl.cmd(l); !strings.HasPrefix(r, "+OK") {
				t.Fatalf("%q -> %q", l, r)
			}
		}
		if got := cl.multi(); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s on %q: got %q, want %q", tc.cmd, tc.data, got, tc.want)
		}
		if r := cl.cmd("NOOP"); r != "+OK" {
			t.Fatalf("stream desync after TOP: %q", r)
		}
	}
}

func admCapaHasSTLS(t *testing.T, cfg *TLSConfig) bool {
	srv := admServer()
	srv.SetTLSConfig(cfg)
	cl, _ := admConn(t, srv, nil)
	cl.line()
	if r := cl.cmd("CAPA"); !strings.HasPrefix(r, "+OK") {
		t.Fatalf("CAPA -> %q", r)
	}
	for _, l := range cl.multi() {
		if l == "STLS" {
			return true
		}
	}
	return false
}

func TestCAPA_STLSOnlyWhenCertLoadable(t *testing.T) {
	if !admCapaHasSTLS(t, admTestCert(t)) {
		t.Fatal("STLS not advertised with a loadable certificate")
	}
	dir := t.TempDir()
	if admCapaHasSTLS(t, &TLSConfig{CertFile: filepath.Join(dir, "no.crt"), KeyFile: filepath.Join(dir, "no.key")}) {
		t.Fatal("STLS advertised although the certificate cannot be loaded")
	}
}
