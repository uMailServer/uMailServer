package imap

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// round82ParseIMAP parses one IMAP value (RFC 3501 §9): NIL -> nil, atoms,
// quoted strings and literals -> string, parenthesised lists ->
// []interface{}. It rejects what a conforming client could not read: bad
// quoted-string escapes, raw CR/LF or 8-bit bytes inside a quoted string.
type round82Parser struct {
	s   string
	pos int
	err error
}

func (p *round82Parser) fail(format string, a ...interface{}) interface{} {
	if p.err == nil {
		p.err = fmt.Errorf("at %d: %s", p.pos, fmt.Sprintf(format, a...))
	}
	return nil
}

func (p *round82Parser) skipSpace() {
	for p.pos < len(p.s) && p.s[p.pos] == ' ' {
		p.pos++
	}
}

func (p *round82Parser) value() interface{} {
	p.skipSpace()
	if p.err != nil || p.pos >= len(p.s) {
		return p.fail("unexpected end")
	}
	switch c := p.s[p.pos]; {
	case c == '(':
		p.pos++
		list := []interface{}{}
		for {
			p.skipSpace()
			if p.pos >= len(p.s) {
				return p.fail("unterminated list")
			}
			if p.s[p.pos] == ')' {
				p.pos++
				return list
			}
			list = append(list, p.value())
			if p.err != nil {
				return nil
			}
		}
	case c == '"':
		p.pos++
		var b strings.Builder
		for p.pos < len(p.s) {
			ch := p.s[p.pos]
			switch {
			case ch == '"':
				p.pos++
				return b.String()
			case ch == '\\':
				if p.pos+1 >= len(p.s) || (p.s[p.pos+1] != '"' && p.s[p.pos+1] != '\\') {
					return p.fail("invalid quoted-string escape")
				}
				b.WriteByte(p.s[p.pos+1])
				p.pos += 2
				continue
			case ch == '\r' || ch == '\n' || ch == 0:
				return p.fail("control character %q in quoted string", ch)
			case ch >= 0x80:
				return p.fail("8-bit byte 0x%02x in quoted string", ch)
			}
			b.WriteByte(ch)
			p.pos++
		}
		return p.fail("unterminated quoted string")
	case c == '{':
		end := strings.Index(p.s[p.pos:], "}\r\n")
		if end < 0 {
			return p.fail("bad literal")
		}
		n, err := strconv.Atoi(p.s[p.pos+1 : p.pos+end])
		start := p.pos + end + 3
		if err != nil || start+n > len(p.s) {
			return p.fail("bad literal length")
		}
		p.pos = start + n
		return p.s[start : start+n]
	default:
		start := p.pos
		for p.pos < len(p.s) && p.s[p.pos] != ' ' && p.s[p.pos] != ')' && p.s[p.pos] != '(' {
			p.pos++
		}
		atom := p.s[start:p.pos]
		if atom == "NIL" {
			return nil
		}
		return atom
	}
}

// round82FetchItem returns the parsed value following "<name> " in the first
// untagged FETCH response of out.
func round82FetchItem(t *testing.T, out []string, name string) (interface{}, error) {
	t.Helper()
	var resp []string
	for _, l := range out {
		if strings.HasPrefix(l, "* 1 FETCH (") || len(resp) > 0 {
			if strings.HasPrefix(l, "a") && strings.Contains(l, " OK ") && len(resp) > 0 {
				break
			}
			resp = append(resp, l)
		}
	}
	full := strings.Join(resp, "\r\n")
	i := strings.Index(full, name+" ")
	if i < 0 {
		return nil, fmt.Errorf("no %s in %q", name, full)
	}
	p := &round82Parser{s: full, pos: i + len(name) + 1}
	v := p.value()
	return v, p.err
}

func TestRound82_ParserSelfCheck(t *testing.T) {
	p := &round82Parser{s: `("a b" NIL {3}` + "\r\n" + `xyz ("\"q\"" ()))`}
	want := []interface{}{"a b", nil, "xyz", []interface{}{`"q"`, []interface{}{}}}
	if got := p.value(); p.err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v err %v", got, p.err)
	}
	for _, bad := range []string{`("a\tb")`, `("caf` + "\xc3\xa9" + `")`, `("x`} {
		bp := &round82Parser{s: bad}
		bp.value()
		if bp.err == nil {
			t.Errorf("%q parsed without error", bad)
		}
	}
}

// F5642: FETCH ENVELOPE rendered the whole From/To header as one personal
// name and local part ("Zed, Q" <a@x> -> mailbox `"Zed, Q" <a`, host `x>`),
// only one address per list, strconv.Quote escapes (\t, é) that no IMAP
// parser accepts, and NIL for Sender/Reply-To/Cc/Bcc/In-Reply-To/Message-ID
// (RFC 3501 §7.4.2).
func TestRound82_FetchEnvelopeRFC3501(t *testing.T) {
	msg := round75Msg(
		`From: "Zed, Q" <a@x>`, "To: Zoe <z@x>, b@y", "Cc: c@z",
		"Subject: caf\xc3\xa9\ttab \"q\"", "Message-ID: <m1@x>", "In-Reply-To: <p0@x>",
		"Date: Tue, 2 Jan 2024 10:00:00 +0000")
	c, _ := round82Server(t, msg)
	c.cmd("s0 SELECT INBOX")

	t.Run("control", func(t *testing.T) {
		v, err := round82FetchItem(t, c.cmd("a1 FETCH 1 (UID RFC822.SIZE)"), "UID")
		if err != nil || v != "1" {
			t.Errorf("UID = %v, %v", v, err)
		}
	})

	from := []interface{}{[]interface{}{"Zed, Q", nil, "a", "x"}}
	want := []interface{}{
		"Tue, 2 Jan 2024 10:00:00 +0000", "caf\xc3\xa9\ttab \"q\"", from, from, from,
		[]interface{}{[]interface{}{"Zoe", nil, "z", "x"}, []interface{}{nil, nil, "b", "y"}},
		[]interface{}{[]interface{}{nil, nil, "c", "z"}}, nil, "<p0@x>", "<m1@x>",
	}
	got, err := round82FetchItem(t, c.cmd("a2 FETCH 1 ENVELOPE"), "ENVELOPE")
	if err != nil {
		t.Errorf("DEFECT F5642: ENVELOPE does not parse: %v", err)
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DEFECT F5642: ENVELOPE\n got %#v\nwant %#v", got, want)
	}
}

// F5642 (continued): flat-metadata fallback for mailstores without header
// access, and a message without Cc/Bcc/Message-ID.
func TestRound82_EnvelopeFormatFallback(t *testing.T) {
	m := &Message{UID: 1, SeqNum: 1, Subject: "plain", Date: "Tue, 2 Jan 2024 10:00:00 +0000", From: "Alice <al@ex.org>", To: "bob@ex.org"}
	out := "* 1 FETCH (" + formatFetchResponse(m, []string{"ENVELOPE"}) + ")"
	p := &round82Parser{s: out, pos: strings.Index(out, "ENVELOPE ") + len("ENVELOPE ")}
	got := p.value()
	from := []interface{}{[]interface{}{"Alice", nil, "al", "ex.org"}}
	want := []interface{}{
		"Tue, 2 Jan 2024 10:00:00 +0000", "plain", from, from, from,
		[]interface{}{[]interface{}{nil, nil, "bob", "ex.org"}}, nil, nil, nil, nil,
	}
	if p.err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("DEFECT F5642: fallback ENVELOPE %q\n got %#v (err %v)\nwant %#v", out, got, p.err, want)
	}
}
