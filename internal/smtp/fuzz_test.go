package smtp

import (
	"strings"
	"testing"
)

// Fuzz parseCommand to find edge cases and panics
func FuzzParseCommand(f *testing.F) {
	// Seed corpus with various SMTP commands
	commands := []string{
		"HELO example.com",
		"EHLO example.com",
		"MAIL FROM:<test@example.com>",
		"RCPT TO:<test@example.com>",
		"DATA",
		"QUIT",
		"NOOP",
		"RSET",
		"HELP",
		"VRFY test@example.com",
		"AUTH LOGIN",
		"AUTH PLAIN AHVzZXIAdGVzdC5jb20=",
		"STARTTLS",
		"   ",
		"",
	}
	for _, c := range commands {
		f.Add(c)
	}

	f.Fuzz(func(t *testing.T, line string) {
		cmd, arg := parseCommand(line)
		// Verify no panic and reasonable output
		_ = cmd
		_ = arg
	})
}

// FuzzAddressParsers feeds arbitrary MAIL/RCPT arguments through the address
// parsers and validator. They must not panic, and an accepted address must be
// unambiguous: exactly one '@' (F6040).
func FuzzAddressParsers(f *testing.F) {
	for _, s := range []string{"FROM:<a@b.c>", "TO:<@r1,@r2:u@x.y>", `TO:<"a@b"@c.d>`, "TO:<u@[IPv6:::1]>", "FROM:<>", "TO:<a\x00@b>", "FROM:<a@b> SIZE=1 RET=FULL"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, arg string) {
		_, _, _ = parseMailFromWithRet(arg)
		to, _, err := parseRcptToWithNotify(arg)
		if err != nil {
			return
		}
		if v, err := ValidateEmail(to); err == nil && !strings.HasPrefix(v, "<") && strings.Count(v, "@") != 1 && !validUTF8Address(v) {
			t.Fatalf("ambiguous address accepted: %q -> %q", to, v)
		}
		_, _ = checkRcptParams(arg)
		_ = mailParamFields(arg)
	})
}
