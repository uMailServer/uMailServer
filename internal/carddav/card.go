package carddav

import (
	"strings"
)

// Card is the simplified contact the webmail edits: the properties it shows,
// read from and written back to the vCard that CardDAV clients share.
type Card struct {
	UID          string   `json:"uid"`
	FullName     string   `json:"name"`
	Emails       []string `json:"emails"`
	Phones       []string `json:"phones"`
	Organization string   `json:"organization,omitempty"`
	Note         string   `json:"note,omitempty"`
}

// CanonicalUser is the storage namespace of a login, the same one the
// CardDAV server uses.
func CanonicalUser(u string) string { return canonicalUser(u) }

// cardProps are the vCard properties a Card owns; MergeCard replaces them
// and keeps every other property of the stored vCard.
var cardProps = map[string]bool{"FN": true, "EMAIL": true, "TEL": true, "ORG": true, "NOTE": true}

// ParseCard reads the Card fields of vCard data.
func ParseCard(data string) Card {
	var c Card
	var n string
	for _, p := range parseVCardProps(data) {
		v := strings.TrimSpace(p.value)
		switch strings.ToUpper(p.name) {
		case "UID":
			c.UID = v
		case "FN":
			c.FullName = unescapeText(v)
		case "N":
			n = v
		case "EMAIL":
			if v != "" {
				c.Emails = append(c.Emails, unescapeText(strings.TrimPrefix(v, "mailto:")))
			}
		case "TEL":
			if v != "" {
				c.Phones = append(c.Phones, unescapeText(strings.TrimPrefix(v, "tel:")))
			}
		case "ORG":
			// ORG is a ;-separated list of units; the organization name is first.
			c.Organization = unescapeText(splitUnescaped(v)[0])
		case "NOTE":
			c.Note = unescapeText(v)
		}
	}
	if c.FullName == "" && n != "" {
		// N: family;given;additional;prefix;suffix
		parts := splitUnescaped(n)
		var name []string
		for _, i := range []int{3, 1, 2, 0, 4} {
			if i < len(parts) && strings.TrimSpace(parts[i]) != "" {
				name = append(name, unescapeText(strings.TrimSpace(parts[i])))
			}
		}
		c.FullName = strings.Join(name, " ")
	}
	return c
}

// NewCardVCard renders c as a new vCard 3.0.
func NewCardVCard(c Card) string {
	lines := []string{"BEGIN:VCARD", "VERSION:3.0", "UID:" + c.UID}
	// vCard 3.0 requires N; the full name goes in the family-name slot.
	lines = append(lines, "N:"+escapeText(c.FullName)+";;;;")
	lines = append(lines, cardLines(c)...)
	lines = append(lines, "END:VCARD")
	return foldLines(lines)
}

// MergeCard writes c's fields into the stored vCard data, keeping every
// property the webmail does not edit (photos, addresses, birthdays, ...)
// so that saving from the webmail never drops what a CardDAV client stored.
func MergeCard(data string, c Card) string {
	var lines []string
	inserted := false
	for _, line := range unfoldVCard(data) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p, ok := parseContentLine(line)
		if ok && cardProps[strings.ToUpper(p.name)] {
			continue
		}
		if ok && strings.EqualFold(p.name, "END") && strings.EqualFold(strings.TrimSpace(p.value), "VCARD") && !inserted {
			lines = append(lines, cardLines(c)...)
			inserted = true
		}
		lines = append(lines, line)
	}
	if !inserted {
		return NewCardVCard(c)
	}
	return foldLines(lines)
}

func cardLines(c Card) []string {
	lines := []string{"FN:" + escapeText(c.FullName)}
	for _, e := range c.Emails {
		lines = append(lines, "EMAIL;TYPE=INTERNET:"+escapeText(e))
	}
	for _, t := range c.Phones {
		lines = append(lines, "TEL:"+escapeText(t))
	}
	if c.Organization != "" {
		lines = append(lines, "ORG:"+escapeText(c.Organization))
	}
	if c.Note != "" {
		lines = append(lines, "NOTE:"+escapeText(c.Note))
	}
	return lines
}

// escapeText escapes a vCard TEXT value (RFC 6350 §3.4).
func escapeText(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ",", `\,`, ";", `\;`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)
	return r.Replace(s)
}

// unescapeText reverses escapeText.
func unescapeText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n', 'N':
				b.WriteByte('\n')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// splitUnescaped splits a structured value on ';' not preceded by '\'.
func splitUnescaped(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == ';' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// foldLines joins content lines with CRLF, folding each at 75 octets
// without splitting a UTF-8 sequence (RFC 6350 §3.2).
func foldLines(lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		width := 75
		for len(line) > width {
			cut := width
			for cut > 0 && line[cut]&0xC0 == 0x80 {
				cut--
			}
			if cut == 0 {
				// Invalid UTF-8 (a run of continuation bytes): there is no
				// character boundary to keep, so cut at the width.
				cut = width
			}
			b.WriteString(line[:cut] + "\r\n ")
			line = line[cut:]
			width = 74 // the leading space counts
		}
		b.WriteString(line + "\r\n")
	}
	return b.String()
}
