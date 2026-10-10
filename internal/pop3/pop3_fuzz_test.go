package pop3

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func FuzzPOP3Helpers(f *testing.F) {
	for _, s := range []string{
		``, `USER alice`, `PASS  pa ss `, `pass`, "PASS\t x", `.`, "a\r\n.\r\n.b\n..c", "\r\n.", "\r.x",
		"RETR 99999999999999999999", "TOP 1 -1", "\x00\xff",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		start := time.Now()
		if pw, ok := passArgument(line); ok && strings.TrimSpace(pw) == "" {
			t.Fatalf("blank password accepted from %q", line)
		}
		if r := redactCommand(line); len(strings.Fields(line)) > 0 &&
			strings.EqualFold(strings.Fields(line)[0], "PASS") && strings.Contains(r, "****") == false {
			t.Fatalf("PASS not redacted: %q", r)
		}
		_ = truncateCommand(line, 10)
		out := dotStuffData([]byte(line))
		// Invariant: unstuffing yields the input and no line is a lone ".".
		var back []byte
		atStart := true
		for i := 0; i < len(out); i++ {
			c := out[i]
			if atStart && c == '.' && i+1 < len(out) && out[i+1] == '.' {
				atStart = false
				continue
			}
			if atStart && c == '.' {
				// stuffed output must never leave a bare leading period
				if !(i+1 < len(out)) || out[i+1] != '.' {
					t.Fatalf("unstuffed leading period in %q -> %q", line, out)
				}
			}
			back = append(back, c)
			atStart = c == '\n' || (atStart && c == '\r')
		}
		if !bytes.Equal(back, []byte(line)) {
			t.Fatalf("dotStuff not reversible: %q -> %q -> %q", line, out, back)
		}
		if time.Since(start) > time.Second {
			t.Fatal("slow")
		}
	})
}
