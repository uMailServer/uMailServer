package sieve

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Run-time limits: a script that exceeds them fails with an error and the
// message gets the implicit keep (RFC 5228 §2.10.6).
const (
	maxRegexInput  = 256 * 1024 // :regex input is truncated to this many bytes
	maxBodyScan    = 1024 * 1024
	maxMIMEDepth   = 8
	maxMIMEParts   = 256
	maxFlags       = 64
	maxRedirects   = 16
	maxActions     = 256
	maxRunOps      = 200000
	maxRunDuration = 5 // seconds
)

type allOfTest []Test
type anyOfTest []Test

type addressTest struct {
	Headers []string
	Part    string // localpart, domain, all
	Match   matchSpec
	Keys    []string
}

type envelopeTest struct {
	Parts []string
	Part  string
	Match matchSpec
	Keys  []string
}

type bodyTest struct {
	Mode  string // raw, text, content
	Types []string
	Match matchSpec
	Keys  []string
}

type hasflagTest struct {
	Match matchSpec
	Keys  []string
}

func asciiLower(s string) string {
	b := []byte(s)
	for idx, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[idx] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// numericCompare implements i;ascii-numeric (RFC 4790 §9.1.1): the leading
// digits are the value; a string without leading digits is larger than any
// number and equal to every other such string.
func numericCompare(a, b string) int {
	da, db := leadingDigits(a), leadingDigits(b)
	switch {
	case da == "" && db == "":
		return 0
	case da == "":
		return 1
	case db == "":
		return -1
	}
	da, db = strings.TrimLeft(da, "0"), strings.TrimLeft(db, "0")
	if len(da) != len(db) {
		if len(da) < len(db) {
			return -1
		}
		return 1
	}
	return strings.Compare(da, db)
}

func leadingDigits(s string) string {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return s[:n]
}

func relationHolds(rel string, c int) bool {
	switch rel {
	case "gt":
		return c > 0
	case "ge":
		return c >= 0
	case "lt":
		return c < 0
	case "le":
		return c <= 0
	case "eq":
		return c == 0
	case "ne":
		return c != 0
	}
	return false
}

// tick charges n operations against the per-run budget.
func (i *Interpreter) tick(n int) error {
	i.ops += n
	if i.ops > maxRunOps {
		return fmt.Errorf("script exceeded the %d operation limit", maxRunOps)
	}
	i.sinceClock += n
	if i.sinceClock >= 256 {
		i.sinceClock = 0
		if !i.deadline.IsZero() && time.Now().After(i.deadline) {
			return fmt.Errorf("script exceeded the %ds run time limit", maxRunDuration)
		}
	}
	return nil
}

// matchValues reports whether any of values matches any of keys under ms.
func (i *Interpreter) matchValues(ms matchSpec, values, keys []string) (bool, error) {
	cmp := ms.Comparator
	if cmp == "" {
		cmp = "i;ascii-casemap"
	}
	fold := cmp == "i;ascii-casemap"
	norm := func(s string) string {
		if fold {
			return asciiLower(s)
		}
		return s
	}
	if ms.Type == "count" {
		n := strconv.Itoa(len(values))
		for _, k := range keys {
			if err := i.tick(1); err != nil {
				return false, err
			}
			if relationHolds(ms.Relation, numericCompare(n, k)) {
				return true, nil
			}
		}
		return false, nil
	}
	for _, v := range values {
		nv := norm(v)
		for _, k := range keys {
			if err := i.tick(1); err != nil {
				return false, err
			}
			ok, err := i.matchOne(ms, cmp, nv, norm(k), v, k, fold)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
	}
	return false, nil
}

func (i *Interpreter) matchOne(ms matchSpec, cmp, nv, nk, rawV, rawK string, fold bool) (bool, error) {
	switch ms.Type {
	case "is":
		if cmp == "i;ascii-numeric" {
			return numericCompare(rawV, rawK) == 0, nil
		}
		return nv == nk, nil
	case "contains":
		return strings.Contains(nv, nk), nil
	case "matches":
		return globMatch(nk, nv, i.timeout)
	case "regex":
		if len(rawV) > maxRegexInput {
			rawV = rawV[:maxRegexInput]
		}
		pat := rawK
		if fold {
			pat = "(?i)" + pat
		}
		re, err := compileCached(pat)
		if err != nil {
			return false, err
		}
		return re.MatchString(rawV), nil
	case "value":
		var c int
		switch cmp {
		case "i;ascii-numeric":
			c = numericCompare(rawV, rawK)
		default:
			c = strings.Compare(nv, nk)
		}
		return relationHolds(ms.Relation, c), nil
	}
	return false, fmt.Errorf("unsupported match type %q", ms.Type)
}

func (i *Interpreter) headerValues(names []string) []string {
	var out []string
	for key, vals := range i.ctx.Headers {
		for _, n := range names {
			if n != "" && strings.EqualFold(key, n) {
				out = append(out, vals...)
				break
			}
		}
	}
	return out
}

func splitAddr(addr, part string) string {
	at := strings.LastIndexByte(addr, '@')
	switch part {
	case "localpart":
		if at < 0 {
			return addr
		}
		return addr[:at]
	case "domain":
		if at < 0 {
			return ""
		}
		return addr[at+1:]
	}
	return addr
}

func (i *Interpreter) evaluateAddressTest(t *addressTest) (bool, error) {
	var values []string
	for _, raw := range i.headerValues(t.Headers) {
		list, err := mail.ParseAddressList(raw)
		if err != nil {
			continue // not an address header value: nothing to test
		}
		for _, a := range list {
			values = append(values, splitAddr(a.Address, t.Part))
		}
	}
	return i.matchValues(t.Match, values, t.Keys)
}

func (i *Interpreter) evaluateEnvelopeTest(t *envelopeTest) (bool, error) {
	var values []string
	for _, p := range t.Parts {
		switch p {
		case "from":
			from := strings.TrimSuffix(strings.TrimPrefix(i.ctx.From, "<"), ">")
			values = append(values, splitAddr(from, t.Part))
		case "to":
			for _, to := range i.ctx.To {
				to = strings.TrimSuffix(strings.TrimPrefix(to, "<"), ">")
				values = append(values, splitAddr(to, t.Part))
			}
		}
	}
	return i.matchValues(t.Match, values, t.Keys)
}

func (i *Interpreter) evaluateHasflagTest(t *hasflagTest) (bool, error) {
	return i.matchValues(t.Match, i.flags, t.Keys)
}

// messageBody returns the body octets of the message, with the header block
// removed when MessageContext.Body holds the whole message.
func (i *Interpreter) messageBody() []byte {
	body := i.ctx.Body
	if len(body) > maxBodyScan {
		body = body[:maxBodyScan]
	}
	if i.ctx.BodyIsFullMessage || looksLikeFullMessage(i.ctx) {
		for _, sep := range []string{"\r\n\r\n", "\n\n"} {
			if idx := bytes.Index(body, []byte(sep)); idx >= 0 {
				return body[idx+len(sep):]
			}
		}
		return nil
	}
	return body
}

// looksLikeFullMessage detects a Body that still carries the header block
// that Headers was parsed from (callers that pass the raw message).
func looksLikeFullMessage(c *SieveContext) bool {
	if len(c.Headers) == 0 || len(c.Body) == 0 {
		return false
	}
	m, err := mail.ReadMessage(bytes.NewReader(c.Body))
	if err != nil || len(m.Header) != len(c.Headers) {
		return false
	}
	for k := range m.Header {
		found := false
		for hk := range c.Headers {
			if strings.EqualFold(k, hk) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

type bodyPart struct {
	ctype string
	text  string
}

func (i *Interpreter) topHeader(name string) string {
	for k, v := range i.ctx.Headers {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

func decodeCTE(cte string, data []byte) []byte {
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		clean := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, data)
		if out, err := base64.StdEncoding.DecodeString(string(clean)); err == nil {
			return out
		}
	case "quoted-printable":
		if out, err := io.ReadAll(io.LimitReader(quotedprintable.NewReader(bytes.NewReader(data)), maxBodyScan)); err == nil {
			return out
		}
	}
	return data
}

func collectParts(ctype, cte string, body []byte, depth int, out *[]bodyPart) {
	if len(*out) >= maxMIMEParts {
		return
	}
	media, params, err := mime.ParseMediaType(ctype)
	if err != nil || ctype == "" {
		media = "text/plain"
	}
	if strings.HasPrefix(media, "multipart/") && params["boundary"] != "" && depth < maxMIMEDepth {
		mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for n := 0; n < maxMIMEParts; n++ {
			p, err := mr.NextRawPart()
			if err != nil {
				return
			}
			data, _ := io.ReadAll(io.LimitReader(p, maxBodyScan))
			collectParts(p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"), data, depth+1, out)
		}
		return
	}
	*out = append(*out, bodyPart{ctype: strings.ToLower(media), text: string(decodeCTE(cte, body))})
}

func typeMatches(ctype string, types []string) bool {
	for _, t := range types {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || t == ctype || (!strings.Contains(t, "/") && strings.HasPrefix(ctype, t+"/")) {
			return true
		}
	}
	return false
}

func (i *Interpreter) evaluateBodyTest(t *bodyTest) (bool, error) {
	raw := i.messageBody()
	var values []string
	if t.Mode == "raw" {
		values = []string{string(raw)}
	} else {
		var parts []bodyPart
		collectParts(i.topHeader("Content-Type"), i.topHeader("Content-Transfer-Encoding"), raw, 0, &parts)
		for _, p := range parts {
			if t.Mode == "text" && !strings.HasPrefix(p.ctype, "text/") {
				continue
			}
			if t.Mode == "content" && !typeMatches(p.ctype, t.Types) {
				continue
			}
			values = append(values, p.text)
		}
	}
	return i.matchValues(t.Match, values, t.Keys)
}

// compileCached compiles pattern through the shared LRU cache.
func compileCached(pattern string) (*regexp.Regexp, error) {
	regexCache.Lock()
	defer regexCache.Unlock()
	if re, ok := regexCache.patterns[pattern]; ok {
		return re, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}
	if len(regexCache.patterns) >= regexCache.maxSize {
		removeCount := regexCache.maxSize / 4
		for n := 0; n < removeCount && len(regexCache.accessOrder) > 0; n++ {
			delete(regexCache.patterns, regexCache.accessOrder[0])
			regexCache.accessOrder = regexCache.accessOrder[1:]
		}
	}
	regexCache.patterns[pattern] = re
	regexCache.accessOrder = append(regexCache.accessOrder, pattern)
	return re, nil
}
