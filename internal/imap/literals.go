package imap

import (
	"errors"
	"io"
	"strings"
)

const (
	// maxArgLiteral bounds one literal that is a command argument (mailbox
	// name, credentials, search string). APPEND message literals have their
	// own, larger limit.
	maxArgLiteral = 64 * 1024
	// maxArgLiteralTotal bounds all argument literals of one command line.
	maxArgLiteralTotal = 256 * 1024
	// maxArgLiteralCount bounds how many literals one command may carry.
	maxArgLiteralCount = 32
)

var errLiteralFatal = errors.New("literal rejected, connection must close")

// trailingLiteral reports whether line ends in a literal marker "{n}" or
// "{n+}" (RFC 3501 §4.3, RFC 7888) and returns the text before it.
func trailingLiteral(line string) (prefix string, n int, nonSync, ok bool) {
	if !strings.HasSuffix(line, "}") {
		return "", 0, false, false
	}
	open := strings.LastIndexByte(line, '{')
	if open < 0 {
		return "", 0, false, false
	}
	size, ns, err := parseLiteralSize(line[open+1 : len(line)-1])
	if err != nil {
		return "", 0, false, false
	}
	return line[:open], size, ns, true
}

// resolveLiterals reads the literals of a command line and folds each into a
// quoted string, so the argument handlers see one logical line. APPEND is
// left alone (it streams its own message literals). F6036: LOGIN, SELECT,
// CREATE, SEARCH ... with a literal argument were never answered with a
// continuation request, so a client sending {n} waited forever or its data
// was executed as commands. The sizes are bounded before anything is
// allocated. ok is false when the command was refused (reply already sent).
func (s *Session) resolveLiterals(line string) (out string, ok bool, err error) {
	f := strings.Fields(line)
	if len(f) < 2 || strings.EqualFold(f[1], "APPEND") {
		return line, true, nil
	}
	tag := f[0]
	total := 0
	for count := 0; ; count++ {
		prefix, n, nonSync, isLit := trailingLiteral(line)
		// A marker inside an open quoted string is not a literal.
		if !isLit || strings.Count(prefix, `"`)%2 == 1 {
			return line, true, nil
		}
		if count >= maxArgLiteralCount || n > maxArgLiteral || total+n > maxArgLiteralTotal {
			if nonSync {
				// The client is already sending the bytes; resync is impossible.
				s.WriteData("BYE Literal too large")
				return "", false, errLiteralFatal
			}
			s.WriteResponse(tag, "BAD [TOOBIG] Literal too large")
			return "", false, nil
		}
		total += n
		if !nonSync {
			s.WriteContinuation("Ready for literal data")
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(s.reader, data); err != nil {
			return "", false, err
		}
		rest, err := s.readLine()
		if err != nil {
			return "", false, err
		}
		if strings.ContainsAny(string(data), "\r\n\x00") {
			// Cannot be carried as a quoted string.
			s.WriteResponse(tag, "BAD Literal with CR, LF or NUL is not supported here")
			return "", false, nil
		}
		line = prefix + quoteMailbox(string(data)) + rest
	}
}
