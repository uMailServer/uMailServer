package imap

import (
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Modified UTF-7 mailbox names (RFC 3501 §5.1.3). Names are stored as UTF-8;
// the wire form is decoded at the command boundary and encoded in responses.

var mutf7Enc = base64.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+,").WithPadding(base64.NoPadding)

var errBadMUTF7 = errors.New("invalid modified UTF-7 mailbox name")

// encodeMUTF7 converts a UTF-8 mailbox name to modified UTF-7.
func encodeMUTF7(name string) string {
	var b strings.Builder
	var run []uint16
	flush := func() {
		if len(run) == 0 {
			return
		}
		buf := make([]byte, 2*len(run))
		for i, u := range run {
			buf[2*i] = byte(u >> 8)
			buf[2*i+1] = byte(u)
		}
		b.WriteByte('&')
		b.WriteString(mutf7Enc.EncodeToString(buf))
		b.WriteByte('-')
		run = run[:0]
	}
	for _, r := range name {
		if r >= 0x20 && r <= 0x7e {
			flush()
			if r == '&' {
				b.WriteString("&-")
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == utf8.RuneError {
			r = 0xFFFD
		}
		if r1, r2 := utf16.EncodeRune(r); r1 != 0xFFFD || r2 != 0xFFFD {
			run = append(run, uint16(r1), uint16(r2))
		} else {
			run = append(run, uint16(r))
		}
	}
	flush()
	return b.String()
}

// decodeMUTF7 converts a modified UTF-7 wire name to UTF-8. It is strict:
// unterminated or non-canonical shifts, odd lengths, unpaired surrogates and
// shifted printable ASCII are rejected. Raw valid UTF-8 is passed through.
func decodeMUTF7(s string) (string, error) {
	if strings.IndexByte(s, '&') < 0 {
		if !utf8.ValidString(s) {
			return "", errBadMUTF7
		}
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c != '&' {
			j := i
			for j < len(s) && s[j] != '&' {
				j++
			}
			if !utf8.ValidString(s[i:j]) {
				return "", errBadMUTF7
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		end := strings.IndexByte(s[i+1:], '-')
		if end < 0 {
			return "", errBadMUTF7
		}
		enc := s[i+1 : i+1+end]
		i += end + 2
		if enc == "" {
			b.WriteByte('&')
			continue
		}
		raw, err := mutf7Enc.Strict().DecodeString(enc)
		if err != nil || len(raw)%2 != 0 {
			return "", errBadMUTF7
		}
		u := make([]uint16, len(raw)/2)
		for k := range u {
			u[k] = uint16(raw[2*k])<<8 | uint16(raw[2*k+1])
		}
		for k := 0; k < len(u); k++ {
			r := rune(u[k])
			switch {
			case utf16.IsSurrogate(r):
				if k+1 >= len(u) {
					return "", errBadMUTF7
				}
				r = utf16.DecodeRune(r, rune(u[k+1]))
				if r == utf8.RuneError {
					return "", errBadMUTF7
				}
				k++
			case r >= 0x20 && r <= 0x7e:
				return "", errBadMUTF7 // printable ASCII must not be shifted
			}
			b.WriteRune(r)
		}
	}
	return b.String(), nil
}

// mboxArg decodes a wire mailbox name and applies the INBOX case rule. On a
// malformed name it answers BAD and reports false.
func (s *Session) mboxArg(arg string) (string, bool) {
	name, err := decodeMUTF7(arg)
	if err != nil {
		s.WriteResponse(s.tag, "BAD "+err.Error())
		return "", false
	}
	return canonMailbox(name), true
}

var systemFlags = map[string]string{
	`\seen`: `\Seen`, `\answered`: `\Answered`, `\flagged`: `\Flagged`,
	`\deleted`: `\Deleted`, `\draft`: `\Draft`,
}

// checkFlags validates client-supplied flags (RFC 3501 §9 flag-keyword /
// flag-extension): system flags are the five settable ones (canonical case;
// \Recent is server-managed), keywords are atoms. It returns the normalised
// list.
func checkFlags(flags []string) ([]string, error) {
	out := make([]string, 0, len(flags))
	for _, f := range flags {
		if strings.HasPrefix(f, `\`) {
			canon, ok := systemFlags[strings.ToLower(f)]
			if !ok {
				return nil, errors.New("invalid or unsupported system flag " + f)
			}
			out = append(out, canon)
			continue
		}
		if len(f) > 255 {
			return nil, errors.New("flag too long")
		}
		for i := 0; i < len(f); i++ {
			c := f[i]
			if c <= 0x20 || c >= 0x7f || strings.IndexByte(`(){%*"\]`, c) >= 0 {
				return nil, errors.New("invalid keyword " + strings.ToValidUTF8(f, "?"))
			}
		}
		out = append(out, f)
	}
	return out, nil
}
