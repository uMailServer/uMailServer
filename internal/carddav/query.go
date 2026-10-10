package carddav

import (
	"errors"
	"strings"
)

// Filter evaluation for the addressbook-query REPORT (RFC 6352 §8.6, §10.5)
// and the minimal vCard content-line parsing it, UID extraction and UID
// rewriting share (F5570, F5571, F5573).

var (
	// errUnsupportedCollation means a text-match named a collation the
	// server does not implement (RFC 6352 §8.3: CARDDAV:supported-collation).
	errUnsupportedCollation = errors.New("unsupported collation")
	// errInvalidFilter means an attribute value outside the RFC 6352 §10.5
	// definitions, or a prop-filter/param-filter without a name.
	errInvalidFilter = errors.New("invalid filter")
)

// QueryFilter is the CARDDAV:filter element (RFC 6352 §10.5).
type QueryFilter struct {
	Test        string       `xml:"test,attr"`
	PropFilters []PropFilter `xml:"prop-filter"`
}

// PropFilter is the CARDDAV:prop-filter element (RFC 6352 §10.5.1).
type PropFilter struct {
	Name         string        `xml:"name,attr"`
	Test         string        `xml:"test,attr"`
	IsNotDefined *struct{}     `xml:"is-not-defined"`
	TextMatches  []TextMatch   `xml:"text-match"`
	ParamFilters []ParamFilter `xml:"param-filter"`
}

// ParamFilter is the CARDDAV:param-filter element (RFC 6352 §10.5.2).
type ParamFilter struct {
	Name         string     `xml:"name,attr"`
	IsNotDefined *struct{}  `xml:"is-not-defined"`
	TextMatch    *TextMatch `xml:"text-match"`
}

// TextMatch is the CARDDAV:text-match element (RFC 6352 §10.5.4).
type TextMatch struct {
	Collation       string `xml:"collation,attr"`
	NegateCondition string `xml:"negate-condition,attr"`
	MatchType       string `xml:"match-type,attr"`
	Value           string `xml:",chardata"`
}

// validTest reports whether a test attribute value is allowed (default anyof).
func validTest(test string) bool {
	return test == "" || test == "anyof" || test == "allof"
}

// validate rejects malformed filters (errInvalidFilter) and collations the
// server does not support (errUnsupportedCollation) before any evaluation,
// so an unsupported collation fails even when no contact would be tested.
func (f *QueryFilter) validate() error {
	if !validTest(f.Test) {
		return errInvalidFilter
	}
	for i := range f.PropFilters {
		pf := &f.PropFilters[i]
		if strings.TrimSpace(pf.Name) == "" || !validTest(pf.Test) {
			return errInvalidFilter
		}
		for j := range pf.TextMatches {
			if err := pf.TextMatches[j].validate(); err != nil {
				return err
			}
		}
		for j := range pf.ParamFilters {
			pa := &pf.ParamFilters[j]
			if strings.TrimSpace(pa.Name) == "" {
				return errInvalidFilter
			}
			if pa.TextMatch != nil {
				if err := pa.TextMatch.validate(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (tm *TextMatch) validate() error {
	switch tm.MatchType {
	case "", "equals", "contains", "starts-with", "ends-with":
	default:
		return errInvalidFilter
	}
	switch tm.NegateCondition {
	case "", "yes", "no":
	default:
		return errInvalidFilter
	}
	switch tm.Collation {
	case "", "default", "i;unicode-casemap", "i;ascii-casemap", "i;octet":
		return nil
	}
	return errUnsupportedCollation
}

// matches reports whether value satisfies the text-match. Without a
// collation attribute (or with "default") i;unicode-casemap applies (RFC 6352
// §8.3, §10.5.4). Unicode case mapping uses simple full-string case folding;
// RFC 5051 NFKD normalisation is not applied.
func (tm *TextMatch) matches(value string) bool {
	needle := tm.Value
	switch tm.Collation {
	case "i;octet":
	case "i;ascii-casemap":
		needle, value = asciiLower(needle), asciiLower(value)
	default:
		needle, value = unicodeFold(needle), unicodeFold(value)
	}
	var ok bool
	switch tm.MatchType {
	case "equals":
		ok = value == needle
	case "starts-with":
		ok = strings.HasPrefix(value, needle)
	case "ends-with":
		ok = strings.HasSuffix(value, needle)
	default: // "contains"
		ok = strings.Contains(value, needle)
	}
	if tm.NegateCondition == "yes" {
		return !ok
	}
	return ok
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func unicodeFold(s string) string {
	return strings.ToLower(strings.ToUpper(s))
}

// matches reports whether a vCard matches the filter. A filter without
// prop-filters matches every address object.
func (f *QueryFilter) matches(props []vcardProp) bool {
	if len(f.PropFilters) == 0 {
		return true
	}
	allOf := f.Test == "allof"
	for i := range f.PropFilters {
		m := f.PropFilters[i].matches(props)
		if allOf && !m {
			return false
		}
		if !allOf && m {
			return true
		}
	}
	return allOf
}

// matches evaluates one prop-filter (RFC 6352 §10.5.1). A name without a
// group prefix matches the property under any group; a grouped name matches
// that exact group. The filter matches when any instance of the property
// satisfies its text-match/param-filter tests.
func (pf *PropFilter) matches(props []vcardProp) bool {
	group, name := "", pf.Name
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		group, name = name[:i], name[i+1:]
	}
	var found []*vcardProp
	for i := range props {
		p := &props[i]
		if strings.EqualFold(p.name, name) && (group == "" || strings.EqualFold(p.group, group)) {
			found = append(found, p)
		}
	}
	if pf.IsNotDefined != nil {
		return len(found) == 0
	}
	if len(found) == 0 {
		return false
	}
	if len(pf.TextMatches) == 0 && len(pf.ParamFilters) == 0 {
		return true
	}
	allOf := pf.Test == "allof"
	for _, p := range found {
		if pf.instanceMatches(p, allOf) {
			return true
		}
	}
	return false
}

func (pf *PropFilter) instanceMatches(p *vcardProp, allOf bool) bool {
	for i := range pf.TextMatches {
		m := pf.TextMatches[i].matches(p.value)
		if allOf && !m {
			return false
		}
		if !allOf && m {
			return true
		}
	}
	for i := range pf.ParamFilters {
		m := pf.ParamFilters[i].matches(p)
		if allOf && !m {
			return false
		}
		if !allOf && m {
			return true
		}
	}
	return allOf
}

// matches evaluates a param-filter against one property (RFC 6352 §10.5.2).
// A multi-valued parameter matches when any of its values matches.
func (pa *ParamFilter) matches(p *vcardProp) bool {
	values, ok := p.params[strings.ToUpper(pa.Name)]
	if pa.IsNotDefined != nil {
		return !ok
	}
	if !ok {
		return false
	}
	if pa.TextMatch == nil {
		return true
	}
	if pa.TextMatch.NegateCondition == "yes" {
		positive := *pa.TextMatch
		positive.NegateCondition = "no"
		for _, v := range values {
			if positive.matches(v) {
				return false
			}
		}
		return true
	}
	for _, v := range values {
		if pa.TextMatch.matches(v) {
			return true
		}
	}
	return false
}

// vcardProp is one unfolded vCard content line: [group "."] name *(";" param) ":" value.
type vcardProp struct {
	group  string
	name   string
	params map[string][]string // upper-cased parameter name -> values
	value  string
}

// unfoldVCard joins folded continuation lines (a line starting with a space
// or tab continues the previous one, RFC 6350 §3.2) and returns the logical
// lines without terminators.
func unfoldVCard(data string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n") {
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(out) > 0 {
			out[len(out)-1] += line[1:]
			continue
		}
		out = append(out, line)
	}
	return out
}

// valueColon returns the index of the ':' separating a content line's name
// and parameters from its value, ignoring colons in quoted parameter values,
// or -1.
func valueColon(line string) int {
	quoted := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			quoted = !quoted
		case ':':
			if !quoted {
				return i
			}
		}
	}
	return -1
}

// splitParams splits the name/parameter part of a content line on ';'
// outside double quotes.
func splitParams(head string) []string {
	var parts []string
	quoted, start := false, 0
	for i := 0; i < len(head); i++ {
		switch head[i] {
		case '"':
			quoted = !quoted
		case ';':
			if !quoted {
				parts = append(parts, head[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, head[start:])
}

// parseContentLine parses one logical content line. ok is false for lines
// without a name/value separator.
func parseContentLine(line string) (vcardProp, bool) {
	line = strings.TrimSpace(line)
	colon := valueColon(line)
	if colon <= 0 {
		return vcardProp{}, false
	}
	parts := splitParams(line[:colon])
	p := vcardProp{name: parts[0], value: line[colon+1:], params: map[string][]string{}}
	if i := strings.LastIndexByte(p.name, '.'); i >= 0 {
		p.group, p.name = p.name[:i], p.name[i+1:]
	}
	for _, param := range parts[1:] {
		key, val, hasVal := strings.Cut(param, "=")
		if !hasVal {
			// vCard 2.1 bare parameter, e.g. TEL;HOME:… means TYPE=HOME.
			key, val = "TYPE", param
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		for _, v := range strings.Split(val, ",") {
			p.params[key] = append(p.params[key], strings.Trim(v, `"`))
		}
	}
	return p, true
}

// parseVCardProps returns every property of the vCard data.
func parseVCardProps(data string) []vcardProp {
	var props []vcardProp
	for _, line := range unfoldVCard(data) {
		if p, ok := parseContentLine(line); ok {
			props = append(props, p)
		}
	}
	return props
}

// isUIDLine reports whether one physical vCard line is the UID property,
// whatever its case, group or parameters (e.g. "uid:x", "UID;VALUE=text:x"),
// and returns its value (F5573). Folded continuation lines are not joined,
// matching the line-oriented UID handling used before.
func isUIDLine(line string) (string, bool) {
	p, ok := parseContentLine(line)
	if !ok || !strings.EqualFold(p.name, "UID") {
		return "", false
	}
	return strings.TrimSpace(p.value), true
}

// rewriteVCardUID replaces the value of the first UID property with uid,
// keeping its group, name spelling, parameters and line terminator (F5573).
// Previously only an exact "UID:<old>" substring was replaced, anywhere in
// the data, so a parameterised or lowercase UID kept the stale value. Data
// without a UID property is returned unchanged.
func rewriteVCardUID(data, uid string) string {
	lines := strings.SplitAfter(data, "\n")
	for i, line := range lines {
		content := strings.TrimRight(line, "\r\n")
		if _, ok := isUIDLine(content); ok {
			lines[i] = content[:valueColon(content)+1] + uid + line[len(content):]
			return strings.Join(lines, "")
		}
	}
	return data
}
