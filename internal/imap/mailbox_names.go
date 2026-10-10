package imap

import (
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// normMailbox returns name in Unicode NFC. F6320: canonically equivalent
// names (U+015E vs S + U+0327) were distinct mailboxes.
func normMailbox(name string) string {
	return norm.NFC.String(name)
}

// resolveStoredName maps an NFC mailbox name to the name actually stored:
// the NFC name itself when that mailbox exists, otherwise a stored name whose
// NFC form equals it (data created before names were normalised). With no
// match the NFC name is returned unchanged.
func (s *Session) resolveStoredName(nfc string) string {
	ascii := true
	for i := 0; i < len(nfc); i++ {
		if nfc[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii || s.server == nil || s.server.mailstore == nil || s.user == "" {
		return nfc
	}
	names, err := s.server.mailstore.ListMailboxes(s.user, "*")
	if err != nil {
		return nfc
	}
	legacy := ""
	for _, n := range names {
		if n == nfc {
			return nfc
		}
		if legacy == "" && normMailbox(n) == nfc {
			legacy = n
		}
	}
	if legacy != "" {
		return legacy
	}
	return nfc
}

// patternForms returns the forms of a decoded LIST pattern to query: NFC and,
// when different, NFD (so legacy decomposed names still match).
func patternForms(p string) []string {
	c := normMailbox(p)
	d := norm.NFD.String(c)
	if d != c {
		return []string{c, d}
	}
	return []string{c}
}

// skipWords returns line with its first n whitespace-delimited words removed.
func skipWords(line string, n int) string {
	i := 0
	for ; n > 0; n-- {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
	}
	return line[i:]
}

// splitQuotedArgs splits an argument string into IMAP arguments: a quoted
// string is one argument with its quotes removed and \\ \" unescaped (so
// "My Folder" is one argument), anything else runs to the next blank (F6032).
func splitQuotedArgs(rest string) []string {
	var out []string
	for i := 0; i < len(rest); {
		switch c := rest[i]; {
		case c == ' ' || c == '\t':
			i++
		case c == '"':
			var b strings.Builder
			i++
			for i < len(rest) && rest[i] != '"' {
				if rest[i] == '\\' && i+1 < len(rest) {
					i++
				}
				b.WriteByte(rest[i])
				i++
			}
			i++ // closing quote
			out = append(out, b.String())
		default:
			j := i
			for j < len(rest) && rest[j] != ' ' && rest[j] != '\t' {
				j++
			}
			out = append(out, rest[i:j])
			i = j
		}
	}
	return out
}

// regroupArgs re-splits the arguments of commands that take mailbox names or
// credentials from the raw line, so quoted strings keep their spaces, quotes
// and apostrophes instead of being cut at blanks by strings.Fields (F6032).
func regroupArgs(command, line string, args []string) []string {
	switch command {
	case "LOGIN", "SELECT", "EXAMINE", "CREATE", "DELETE", "RENAME", "SUBSCRIBE",
		"UNSUBSCRIBE", "STATUS", "LSUB", "COPY", "MOVE", "APPEND":
		return splitQuotedArgs(skipWords(line, 2))
	case "UID":
		if len(args) > 0 {
			if sub := strings.ToUpper(args[0]); sub == "COPY" || sub == "MOVE" {
				return append([]string{args[0]}, splitQuotedArgs(skipWords(line, 3))...)
			}
		}
	}
	return args
}

// canonMailbox applies the one case rule of RFC 3501 §5.1: INBOX (and the
// hierarchy below it) is case-insensitive, every other name is case-sensitive.
func canonMailbox(name string) string {
	if len(name) >= 5 && strings.EqualFold(name[:5], "INBOX") && (len(name) == 5 || name[5] == '/') {
		return "INBOX" + name[5:]
	}
	return name
}

// checkNewMailboxName validates a name for CREATE / RENAME target and returns
// it normalised (trailing hierarchy delimiters dropped, INBOX canonical).
func checkNewMailboxName(name string) (string, error) {
	name = canonMailbox(strings.TrimRight(name, "/"))
	if name == "" {
		return "", fmt.Errorf("Invalid mailbox name")
	}
	if strings.ContainsAny(name, "*%") {
		return "", fmt.Errorf("Mailbox name must not contain wildcards")
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" {
			return "", fmt.Errorf("Invalid mailbox name: empty hierarchy level")
		}
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("Invalid mailbox name: control character")
		}
	}
	return name, nil
}

// mailboxExists reports whether the user has a mailbox named exactly name.
func (s *Session) mailboxExists(name string) (bool, error) {
	if name == "INBOX" {
		return true, nil
	}
	names, err := s.server.mailstore.ListMailboxes(s.user, "*")
	if err != nil {
		return false, err
	}
	for _, n := range names {
		if n == name {
			return true, nil
		}
	}
	return false, nil
}

// quoteMailbox renders a mailbox name as an IMAP quoted string.
func quoteMailbox(name string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
}
