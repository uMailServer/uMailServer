package imap

import (
	"strings"
	"testing"
)

// F6037: the parsers must never panic on arbitrary client input.
func FuzzIMAPArgParsers(f *testing.F) {
	for _, seed := range []string{
		`ALL`, `NOT FROM "a b" OR SEEN (SUBJECT x)`, `HEADER Subject "x\"y"`, `1:*,5:3`,
		`UID 1:*`, `((((`, `"unterminated`, `{5+}`, `SINCE 1-Jan-2020 BEFORE 31-Dec-2025`,
		`(REVERSE DATE SUBJECT) UTF-8 ALL`, "\x00\xff\\", `OR OR OR`, `NOT NOT`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		toks := tokenizeIMAPArgs(in)
		_, _ = parseSearchProgram(toks)
		parts := strings.Fields(in)
		_ = parseSearchCriteria(parts)
		if len(parts) > 0 {
			_ = checkSearchKey(parts)
			_, _ = parseSortCriteria(parts)
		}
		_, _ = ParseSequenceSet(in)
		_ = splitQuotedArgs(in)
		_ = regroupArgs("STATUS", "a STATUS "+in, parts)
		_ = canonMailbox(in)
		_, _ = checkNewMailboxName(in)
		_ = baseSubject(in)
		_, _, _, _ = trailingLiteral(in)
		_, _ = parseListRequest(toks)
	})
}

// F6037: whole-command fuzzing against a mock store in the Selected state.
func FuzzHandleCommand(f *testing.F) {
	for _, seed := range []string{
		`a1 SELECT "INBOX"`, `a2 STATUS INBOX (MESSAGES UNSEEN)`, `a3 SEARCH NOT (OR FROM x SUBJECT "y z")`,
		`a4 UID FETCH 1:* (FLAGS BODY[HEADER.FIELDS (SUBJECT)]<0.10>)`, `a5 STORE 1:* +FLAGS (\Seen)`,
		`a6 RENAME "a b" c`, `a7 LIST "" "%"`, `a8 UID SORT (ARRIVAL) UTF-8 ALL`, `a9 THREAD REFERENCES UTF-8 ALL`,
		`b1 COPY 1:* "x y"`, `b2 UID EXPUNGE 1:*`, `b3 FETCH`, `b4 UID`, `b5 SEARCH CHARSET`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			switch strings.ToUpper(fields[1]) {
			case "STARTTLS", "COMPRESS", "IDLE", "LOGOUT", "AUTHENTICATE", "APPEND":
				return
			}
		}
		if len(line) > 4096 {
			return
		}
		mock := newMockConn("")
		server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
		session := NewSession(mock, server)
		session.state = StateSelected
		session.user = "test"
		session.selected = &Mailbox{Name: "INBOX", Exists: 3}
		_ = session.handleCommand(line)
	})
}
