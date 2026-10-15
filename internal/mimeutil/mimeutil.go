// Package mimeutil decodes the parts of a message header that are shown to
// people: RFC 2047 encoded-words and RFC 5322 address lists. Stored message
// metadata keeps the header exactly as received (IMAP ENVELOPE must be raw),
// so the web, REST and JMAP presentation layers decode it with this package.
package mimeutil

import (
	"errors"
	"io"
	"mime"
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Address is one parsed mailbox: the display name (decoded, possibly empty)
// and the addr-spec.
type Address struct {
	Name  string
	Email string
}

// DecodeCharset converts b, labelled with charset, to UTF-8. It covers UTF-8,
// US-ASCII, the Latin-1 and Latin-5 (Turkish) families and windows-1252, which
// is what Go's mime package lacks; any other bytes are returned as UTF-8 with
// invalid sequences replaced by a space.
func DecodeCharset(b []byte, charset string) string {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "iso-8859-1", "latin1", "iso8859-1", "l1":
		return mapBytes(b, nil)
	case "windows-1252", "cp1252":
		return mapBytes(b, windows1252)
	case "iso-8859-9", "iso8859-9", "latin5", "l5", "windows-1254", "cp1254":
		// Turkish: Latin-1 except six code points (F7511). windows-1254 adds
		// the windows-1252 C1 block on top.
		var high map[byte]rune
		if c := strings.ToLower(strings.TrimSpace(charset)); c == "windows-1254" || c == "cp1254" {
			high = windows1252
		}
		return mapBytes(b, mergeTurkish(high))
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return strings.ToValidUTF8(string(b), " ")
}

var turkish = map[byte]rune{0xD0: 'Ğ', 0xDD: 'İ', 0xDE: 'Ş', 0xF0: 'ğ', 0xFD: 'ı', 0xFE: 'ş'}

func mergeTurkish(base map[byte]rune) map[byte]rune {
	out := make(map[byte]rune, len(base)+len(turkish))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range turkish {
		out[k] = v
	}
	return out
}

// windows1252 maps the bytes 0x80-0x9F, where windows-1252 differs from Latin-1.
var windows1252 = map[byte]rune{
	0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡',
	0x88: 'ˆ', 0x89: '‰', 0x8A: 'Š', 0x8B: '‹', 0x8C: 'Œ', 0x8E: 'Ž',
	0x91: '‘', 0x92: '’', 0x93: '“', 0x94: '”', 0x95: '•', 0x96: '–', 0x97: '—',
	0x98: '˜', 0x99: '™', 0x9A: 'š', 0x9B: '›', 0x9C: 'œ', 0x9E: 'ž', 0x9F: 'Ÿ',
}

// mapBytes widens single-byte text to UTF-8, applying overrides on top of Latin-1.
func mapBytes(b []byte, overrides map[byte]rune) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
		if o, ok := overrides[c]; ok {
			r[i] = o
		}
	}
	return string(r)
}

// knownCharset reports whether DecodeCharset converts charset itself.
func knownCharset(charset string) bool {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "iso-8859-1", "latin1", "iso8859-1", "l1", "windows-1252", "cp1252",
		"iso-8859-9", "iso8859-9", "latin5", "l5", "windows-1254", "cp1254":
		return true
	}
	return false
}

// wordDecoder decodes encoded-words for display. A charset it cannot convert
// is accepted when the bytes are valid UTF-8 (a mislabelled word). Otherwise a
// strict decoder fails, so the whole header stays visibly encoded instead of
// turning into replacement characters; a lenient one (display names in an
// address list) substitutes U+FFFD, because one unreadable name must not make
// the whole list unparseable and hide the other recipients.
func wordDecoder(lenient bool) *mime.WordDecoder {
	return &mime.WordDecoder{CharsetReader: func(charset string, in io.Reader) (io.Reader, error) {
		bs, err := io.ReadAll(in)
		if err != nil {
			return nil, err
		}
		if !knownCharset(charset) && !utf8.Valid(bs) {
			if !lenient {
				return nil, errUnknownCharset
			}
			return strings.NewReader(strings.ToValidUTF8(string(bs), "\uFFFD")), nil
		}
		return strings.NewReader(DecodeCharset(bs, charset)), nil
	}}
}

var errUnknownCharset = errors.New("mimeutil: unsupported charset")

// cleanText replaces control characters (other than tab) with a space: a
// decoded header can carry CR, LF or NUL that were encoded to hide them.
func cleanText(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 0x20 && c != '\t') || c == 0x7f {
			return strings.Map(func(r rune) rune {
				if (r < 0x20 && r != '\t') || r == 0x7f {
					return ' '
				}
				return r
			}, s)
		}
	}
	return s
}

// DecodeHeader decodes the RFC 2047 encoded-words of a header value. A value
// that cannot be decoded is returned unchanged.
func DecodeHeader(v string) string {
	if !strings.Contains(v, "=?") {
		return cleanText(v)
	}
	if out, err := wordDecoder(false).DecodeHeader(v); err == nil {
		return cleanText(out)
	}
	return cleanText(v)
}

// ParseAddresses parses an address-list header value (To, Cc, From, ...) and
// decodes the display names. ok is false when the value is not a valid list,
// in which case the caller should fall back to the raw text.
func ParseAddresses(raw string) ([]Address, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	p := mail.AddressParser{WordDecoder: wordDecoder(true)}
	list, err := p.ParseList(raw)
	if err != nil || len(list) == 0 {
		return nil, false
	}
	out := make([]Address, 0, len(list))
	for _, a := range list {
		out = append(out, Address{Name: cleanText(a.Name), Email: a.Address})
	}
	return out, true
}

// Display formats a for people: `Name <addr>` (the name quoted when it holds
// RFC 5322 specials) or just the addr-spec.
func (a Address) Display() string {
	if a.Name == "" {
		return a.Email
	}
	// Angle brackets in a name would give the displayed address a second
	// <...>: a client that takes the first one would reply to the wrong
	// address. Typographic brackets keep the name readable.
	name := strings.NewReplacer("<", "‹", ">", "›").Replace(a.Name)
	if strings.ContainsAny(name, `()<>[]:;@\,."`) {
		name = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
	}
	return name + " <" + a.Email + ">"
}

// DisplayList returns the display form of each address in an address-list
// header value. A value that does not parse is returned as one decoded element
// (nil for an empty value) rather than being split on commas that may sit
// inside quoted names.
func DisplayList(raw string) []string {
	list, ok := ParseAddresses(raw)
	if !ok {
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		return []string{DecodeHeader(raw)}
	}
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.Display())
	}
	return out
}

// DisplayText formats an address-list header value for people as one string:
// the addresses in display form joined by ", ". A value that does not parse is
// returned decoded (DecodeHeader).
func DisplayText(raw string) string {
	list, ok := ParseAddresses(raw)
	if !ok || len(list) == 0 {
		return DecodeHeader(raw)
	}
	parts := make([]string, len(list))
	for i, a := range list {
		parts[i] = a.Display()
	}
	return strings.Join(parts, ", ")
}
