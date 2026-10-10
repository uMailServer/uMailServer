package sieve

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

// Compile-time limits. A script that exceeds them is rejected so a hostile
// script cannot exhaust memory or CPU when it later runs.
const (
	maxScriptSize  = 1024 * 1024 // matches the ManageSieve PUTSCRIPT cap
	maxPatternLen  = 1024        // :regex pattern and :matches key length
	maxKeyLen      = 4096        // any key / string-list element
	maxListEntries = 1000        // elements of one string list or test list
)

// supportedExtensions lists the capabilities this interpreter implements.
// RFC 5228 §3.2: requiring anything else must fail the script. F5036.
// F5343: "variables" (RFC 5229) is not listed: ${name} expansion and the
// string test are not implemented, so requiring it must fail the script.
var supportedExtensions = map[string]bool{
	"fileinto":                   true,
	"reject":                     true,
	"vacation":                   true,
	"vacation-seconds":           true,
	"envelope":                   true,
	"body":                       true,
	"relational":                 true,
	"regex":                      true,
	"imap4flags":                 true,
	"copy":                       true,
	"mailbox":                    true,
	"comparator-i;octet":         true,
	"comparator-i;ascii-casemap": true,
	"comparator-i;ascii-numeric": true,
}

type argKind int

const (
	kNone argKind = iota
	kString
	kNumber
	kStrList // a string or a bracketed string list
)

type tagSpec struct {
	kind argKind
	ext  string // extension that must be required to use the tag
}

type cmdSpec struct {
	ext  string
	tags map[string]tagSpec
	pos  []argKind
}

// parsedArgs is the typed form of a command or test argument sequence.
type parsedArgs struct {
	tags map[string]Value
	pos  []Value
}

func (pa parsedArgs) has(tag string) bool { _, ok := pa.tags[tag]; return ok }

func (pa parsedArgs) str(tag string) string {
	if sv, ok := pa.tags[tag].(*StringValue); ok {
		return sv.Value
	}
	return ""
}

// listOf flattens a kStrList value.
func listOf(v Value) []string {
	switch t := v.(type) {
	case *StringValue:
		return []string{t.Value}
	case *ListValue:
		return t.Values
	}
	return nil
}

// fullArgs returns the arguments of cmd with the parser's cmd.Tag put back in
// front (the parser stores the first tag of a command separately).
func fullArgs(cmd *Command) []Value {
	if cmd.Tag == "" {
		return cmd.Arguments
	}
	return append([]Value{&TagValue{Value: cmd.Tag}}, cmd.Arguments...)
}

// env is the set of extensions a script has required.
type env map[string]bool

func (e env) need(ext, what string) error {
	if ext != "" && !e[ext] {
		return fmt.Errorf("%s requires: require %q;", what, ext)
	}
	return nil
}

func checkKind(v Value, k argKind) error {
	switch k {
	case kString:
		if sv, ok := v.(*StringValue); ok && !sv.Bare && len(sv.Value) <= maxKeyLen*16 {
			return nil
		}
		return fmt.Errorf("expected a string")
	case kNumber:
		if nv, ok := v.(*NumberValue); ok && nv.Value >= 0 {
			return nil
		}
		return fmt.Errorf("expected a non-negative number")
	case kStrList:
		switch t := v.(type) {
		case *StringValue:
			if !t.Bare && len(t.Value) <= maxKeyLen*16 {
				return nil
			}
		case *ListValue:
			if len(t.Values) > maxListEntries {
				return fmt.Errorf("string list longer than %d entries", maxListEntries)
			}
			for _, s := range t.Values {
				if len(s) > maxKeyLen*16 {
					return fmt.Errorf("string list element too long")
				}
			}
			return nil
		}
		return fmt.Errorf("expected a string or string list")
	}
	return nil
}

// parseArgs types an argument sequence against spec: tagged arguments first,
// then exactly the positional arguments. RFC 5228 §2.6.
func parseArgs(name string, args []Value, spec cmdSpec, e env) (parsedArgs, error) {
	pa := parsedArgs{tags: map[string]Value{}}
	if err := e.need(spec.ext, name); err != nil {
		return pa, err
	}
	for idx := 0; idx < len(args); idx++ {
		tv, isTag := args[idx].(*TagValue)
		if !isTag {
			pa.pos = append(pa.pos, args[idx])
			continue
		}
		if len(pa.pos) > 0 {
			return pa, fmt.Errorf("%s: tagged argument :%s after positional arguments", name, tv.Value)
		}
		tag := strings.ToLower(tv.Value)
		ts, ok := spec.tags[tag]
		if !ok {
			return pa, fmt.Errorf("%s: unknown tagged argument :%s", name, tv.Value)
		}
		if err := e.need(ts.ext, name+" :"+tag); err != nil {
			return pa, err
		}
		if _, dup := pa.tags[tag]; dup {
			return pa, fmt.Errorf("%s: duplicate tagged argument :%s", name, tag)
		}
		if ts.kind == kNone {
			pa.tags[tag] = nil
			continue
		}
		idx++
		if idx >= len(args) {
			return pa, fmt.Errorf("%s: :%s needs an argument", name, tag)
		}
		if err := checkKind(args[idx], ts.kind); err != nil {
			return pa, fmt.Errorf("%s: :%s: %w", name, tag, err)
		}
		pa.tags[tag] = args[idx]
	}
	if len(pa.pos) != len(spec.pos) {
		return pa, fmt.Errorf("%s: expected %d positional argument(s), got %d", name, len(spec.pos), len(pa.pos))
	}
	for idx, k := range spec.pos {
		if err := checkKind(pa.pos[idx], k); err != nil {
			return pa, fmt.Errorf("%s: argument %d: %w", name, idx+1, err)
		}
	}
	return pa, nil
}

var cmdSpecs = map[string]cmdSpec{
	"require": {pos: []argKind{kStrList}},
	"stop":    {},
	"discard": {},
	"keep":    {tags: map[string]tagSpec{"flags": {kStrList, "imap4flags"}}},
	"fileinto": {ext: "fileinto", pos: []argKind{kString}, tags: map[string]tagSpec{
		"copy": {kNone, "copy"}, "flags": {kStrList, "imap4flags"}, "create": {kNone, "mailbox"}}},
	"redirect": {pos: []argKind{kString}, tags: map[string]tagSpec{"copy": {kNone, "copy"}}},
	"reject":   {ext: "reject", pos: []argKind{kString}},
	"vacation": {ext: "vacation", pos: []argKind{kString}, tags: map[string]tagSpec{
		"days": {kNumber, ""}, "seconds": {kNumber, "vacation-seconds"}, "subject": {kString, ""},
		"from": {kString, ""}, "addresses": {kStrList, ""}, "mime": {kNone, ""}, "handle": {kString, ""}}},
	"addflag":    {ext: "imap4flags", pos: []argKind{kStrList}},
	"setflag":    {ext: "imap4flags", pos: []argKind{kStrList}},
	"removeflag": {ext: "imap4flags", pos: []argKind{kStrList}},
}

func matchTags(extra map[string]tagSpec) map[string]tagSpec {
	t := map[string]tagSpec{
		"is": {kNone, ""}, "contains": {kNone, ""}, "matches": {kNone, ""},
		"regex": {kNone, "regex"}, "count": {kString, "relational"}, "value": {kString, "relational"},
		"comparator": {kString, ""},
	}
	for k, v := range extra {
		t[k] = v
	}
	return t
}

var addrPartTags = map[string]tagSpec{"localpart": {kNone, ""}, "domain": {kNone, ""}, "all": {kNone, ""}}

var testSpecs = map[string]cmdSpec{
	"exists":   {pos: []argKind{kStrList}},
	"size":     {tags: map[string]tagSpec{"over": {kNumber, ""}, "under": {kNumber, ""}}},
	"header":   {tags: matchTags(nil), pos: []argKind{kStrList, kStrList}},
	"address":  {tags: matchTags(addrPartTags), pos: []argKind{kStrList, kStrList}},
	"envelope": {ext: "envelope", tags: matchTags(addrPartTags), pos: []argKind{kStrList, kStrList}},
	"body": {ext: "body", pos: []argKind{kStrList}, tags: matchTags(map[string]tagSpec{
		"raw": {kNone, ""}, "text": {kNone, ""}, "content": {kStrList, ""}})},
	"hasflag": {ext: "imap4flags", tags: matchTags(nil), pos: []argKind{kStrList}},
}

// matchSpec is a resolved match type + comparator (RFC 5228 §2.7, RFC 5231,
// RFC 5260 regex).
type matchSpec struct {
	Type       string // is, contains, matches, regex, count, value
	Relation   string // gt ge lt le eq ne for count/value
	Comparator string // i;ascii-casemap, i;octet, i;ascii-numeric
}

func (m matchSpec) numeric() bool {
	return m.Comparator == "i;ascii-numeric" || m.Type == "count"
}

func normComparator(c string) (string, error) {
	switch c {
	case "":
		return "i;ascii-casemap", nil
	case "i;ascii-casemap", "i;octet", "i;ascii-numeric":
		return c, nil
	}
	return "", fmt.Errorf("unsupported comparator %q", c)
}

// buildMatch resolves the match-type and :comparator tags of a test and
// validates the literal keys.
func buildMatch(name string, pa parsedArgs, e env, keys []string) (matchSpec, error) {
	ms := matchSpec{Type: "is"}
	found := 0
	for _, t := range []string{"is", "contains", "matches", "regex", "count", "value"} {
		if pa.has(t) {
			ms.Type = t
			found++
		}
	}
	if found > 1 {
		return ms, fmt.Errorf("%s: more than one match type", name)
	}
	if ms.Type == "count" || ms.Type == "value" {
		ms.Relation = strings.ToLower(pa.str(ms.Type))
		switch ms.Relation {
		case "gt", "ge", "lt", "le", "eq", "ne":
		default:
			return ms, fmt.Errorf("%s: invalid relational operator %q", name, pa.str(ms.Type))
		}
	}
	cmp, err := normComparator(pa.str("comparator"))
	if err != nil {
		return ms, fmt.Errorf("%s: %w", name, err)
	}
	if cmp == "i;ascii-numeric" {
		if err := e.need("comparator-i;ascii-numeric", name+" :comparator \"i;ascii-numeric\""); err != nil {
			return ms, err
		}
		if ms.Type == "contains" || ms.Type == "matches" || ms.Type == "regex" {
			return ms, fmt.Errorf("%s: comparator i;ascii-numeric does not support :%s", name, ms.Type)
		}
	}
	ms.Comparator = cmp
	for _, k := range keys {
		switch ms.Type {
		case "regex":
			if len(k) > maxPatternLen {
				return ms, fmt.Errorf("%s: regex longer than %d bytes", name, maxPatternLen)
			}
			if _, err := regexp.Compile(k); err != nil {
				return ms, fmt.Errorf("%s: invalid regex %q: %v", name, k, err)
			}
		case "matches":
			if len(k) > maxPatternLen {
				return ms, fmt.Errorf("%s: :matches key longer than %d bytes", name, maxPatternLen)
			}
		}
	}
	return ms, nil
}

// identName returns the lower-cased test name of a flat test sequence.
func identName(args []Value) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("missing test")
	}
	sv, ok := args[0].(*StringValue)
	if !ok || sv.Quoted || sv.IsLiteral {
		return "", fmt.Errorf("unsupported test: expected a test name")
	}
	return strings.ToLower(sv.Value), nil
}

func addrPartOf(pa parsedArgs) (string, error) {
	part, n := "all", 0
	for _, p := range []string{"localpart", "domain", "all"} {
		if pa.has(p) {
			part = p
			n++
		}
	}
	if n > 1 {
		return "", fmt.Errorf("more than one address part")
	}
	return part, nil
}

// buildTest compiles the flat argument sequence of one test (name first)
// into a Test node, rejecting every RFC 5228 §2.10 error. depth bounds
// not/allof/anyof nesting.
func buildTest(args []Value, e env, depth int) (Test, error) {
	if depth > MaxNesting {
		return nil, fmt.Errorf("tests nested deeper than %d levels", MaxNesting)
	}
	name, err := identName(args)
	if err != nil {
		return nil, err
	}
	rest := args[1:]
	switch name {
	case "true", "false":
		if len(rest) != 0 {
			return nil, fmt.Errorf("%s test takes no arguments", name)
		}
		return constTest(name == "true"), nil
	case "not":
		inner, err := buildTest(rest, e, depth+1)
		if err != nil {
			return nil, err
		}
		return notTest{inner}, nil
	case "allof", "anyof":
		if len(rest) != 1 {
			return nil, fmt.Errorf("%s needs a parenthesised test list", name)
		}
		tl, ok := rest[0].(*TestListValue)
		if !ok || len(tl.Tests) == 0 {
			return nil, fmt.Errorf("%s needs a parenthesised test list", name)
		}
		if len(tl.Tests) > maxListEntries {
			return nil, fmt.Errorf("%s: more than %d tests", name, maxListEntries)
		}
		subs := make([]Test, 0, len(tl.Tests))
		for _, sub := range tl.Tests {
			t, err := buildTest(sub, e, depth+1)
			if err != nil {
				return nil, err
			}
			subs = append(subs, t)
		}
		if name == "allof" {
			return allOfTest(subs), nil
		}
		return anyOfTest(subs), nil
	}
	spec, ok := testSpecs[name]
	if !ok {
		return nil, fmt.Errorf("unsupported test %q", name)
	}
	pa, err := parseArgs(name+" test", rest, spec, e)
	if err != nil {
		return nil, err
	}
	switch name {
	case "exists":
		return existsTest(listOf(pa.pos[0])), nil
	case "size":
		over, under := pa.has("over"), pa.has("under")
		if over == under {
			return nil, fmt.Errorf("size test needs exactly one of :over or :under")
		}
		if over {
			return &SizeTest{Relation: ":over", Size: pa.tags["over"].(*NumberValue).Value}, nil
		}
		return &SizeTest{Relation: ":under", Size: pa.tags["under"].(*NumberValue).Value}, nil
	}
	keys := listOf(pa.pos[len(pa.pos)-1])
	ms, err := buildMatch(name, pa, e, keys)
	if err != nil {
		return nil, err
	}
	switch name {
	case "header":
		return &HeaderTest{Headers: listOf(pa.pos[0]), KeyList: keys, MatchType: ":" + ms.Type,
			Relation: ms.Relation, Comparator: ms.Comparator}, nil
	case "address":
		part, err := addrPartOf(pa)
		if err != nil {
			return nil, fmt.Errorf("address: %w", err)
		}
		return &addressTest{Headers: listOf(pa.pos[0]), Part: part, Match: ms, Keys: keys}, nil
	case "envelope":
		part, err := addrPartOf(pa)
		if err != nil {
			return nil, fmt.Errorf("envelope: %w", err)
		}
		parts := listOf(pa.pos[0])
		for idx, p := range parts {
			p = strings.ToLower(p)
			if p != "from" && p != "to" {
				return nil, fmt.Errorf("envelope: unsupported envelope part %q", parts[idx])
			}
			parts[idx] = p
		}
		return &envelopeTest{Parts: parts, Part: part, Match: ms, Keys: keys}, nil
	case "body":
		bt := &bodyTest{Mode: "text", Match: ms, Keys: keys}
		n := 0
		for _, m := range []string{"raw", "text", "content"} {
			if pa.has(m) {
				bt.Mode = m
				n++
			}
		}
		if n > 1 {
			return nil, fmt.Errorf("body: only one of :raw, :text, :content allowed")
		}
		if bt.Mode == "content" {
			bt.Types = listOf(pa.tags["content"])
		}
		return bt, nil
	case "hasflag":
		return &hasflagTest{Match: ms, Keys: keys}, nil
	}
	return nil, fmt.Errorf("unsupported test %q", name)
}

// Compile parses and validates a Sieve script. Every RFC 5228 §2.10 error
// (unknown command/test/tag, wrong argument types or counts, unsupported or
// missing `require`, misplaced else/elsif/require, excessive nesting) is
// reported here so a broken script is never stored or activated.
func Compile(source string) (*Script, error) {
	if len(source) > maxScriptSize {
		return nil, fmt.Errorf("script larger than %d bytes", maxScriptSize)
	}
	s, err := NewParser(source).Parse()
	if err != nil {
		return nil, err
	}
	if err := Validate(s); err != nil {
		return nil, err
	}
	return s, nil
}

// Validate checks a parsed script as Compile does and records the required
// extensions on it. It must be called before the script is shared between
// goroutines.
func Validate(s *Script) error {
	e, err := checkScript(s)
	if err != nil {
		return err
	}
	s.exts = e
	return nil
}

// checkScript validates s and returns its required extension set.
func checkScript(s *Script) (env, error) {
	e := env{}
	idx := 0
	for ; idx < len(s.Commands) && s.Commands[idx].Name == "require"; idx++ {
		cmd := &s.Commands[idx]
		if cmd.Block != nil {
			return nil, fmt.Errorf("require: unexpected block")
		}
		exts, err := requireExtensions(cmd)
		if err != nil {
			return nil, err
		}
		for _, x := range exts {
			e[x] = true
		}
	}
	if err := checkCommands(s.Commands[idx:], e, 0); err != nil {
		return nil, err
	}
	return e, nil
}

func checkCommands(cmds []Command, e env, depth int) error {
	if depth > MaxNesting {
		return fmt.Errorf("blocks nested deeper than %d levels", MaxNesting)
	}
	prev := "" // previous command of the sequence, for elsif/else chains
	for idx := range cmds {
		cmd := &cmds[idx]
		switch cmd.Name {
		case "if", "elsif", "else":
			if cmd.Name != "if" && prev != "if" && prev != "elsif" {
				return fmt.Errorf("%s without preceding if", cmd.Name)
			}
			if cmd.Block == nil {
				return fmt.Errorf("%s: missing block", cmd.Name)
			}
			if cmd.Name == "else" {
				if len(fullArgs(cmd)) != 0 {
					return fmt.Errorf("else takes no arguments")
				}
			} else if _, err := buildTest(fullArgs(cmd), e, 0); err != nil {
				return fmt.Errorf("%s: %w", cmd.Name, err)
			}
			if err := checkCommands(cmd.Block.Commands, e, depth+1); err != nil {
				return err
			}
		case "require":
			return fmt.Errorf("require must be at the start of the script")
		default:
			if _, err := compileCommand(cmd, e); err != nil {
				return err
			}
		}
		prev = cmd.Name
	}
	return nil
}

// compileCommand validates a non-conditional command and returns its typed
// arguments.
func compileCommand(cmd *Command, e env) (parsedArgs, error) {
	spec, ok := cmdSpecs[cmd.Name]
	if !ok || cmd.Name == "require" {
		return parsedArgs{}, fmt.Errorf("unknown command %q", cmd.Name)
	}
	if cmd.Block != nil {
		return parsedArgs{}, fmt.Errorf("%s: unexpected block", cmd.Name)
	}
	if cmd.Name == "vacation" && e["vacation-seconds"] && !e["vacation"] {
		// RFC 6131 builds on RFC 5230; scripts that only name
		// "vacation-seconds" were accepted before and stay valid.
		e = env{"vacation": true, "vacation-seconds": true}
	}
	pa, err := parseArgs(cmd.Name, fullArgs(cmd), spec, e)
	if err != nil {
		return pa, err
	}
	switch cmd.Name {
	case "redirect":
		if _, err := mail.ParseAddress(pa.pos[0].(*StringValue).Value); err != nil {
			return pa, fmt.Errorf("invalid redirect address: %s", pa.pos[0].(*StringValue).Value)
		}
	case "vacation":
		if pa.has("days") && pa.has("seconds") {
			return pa, fmt.Errorf("vacation: :days and :seconds are mutually exclusive")
		}
	}
	return pa, nil
}

// requireExtensions returns the extension names named by a require command,
// or an error if any of them is not supported.
func requireExtensions(cmd *Command) ([]string, error) {
	var exts []string
	for _, arg := range fullArgs(cmd) {
		switch v := arg.(type) {
		case *StringValue:
			if v.Bare {
				return nil, fmt.Errorf("require: invalid argument")
			}
			exts = append(exts, v.Value)
		case *ListValue:
			exts = append(exts, v.Values...)
		default:
			return nil, fmt.Errorf("require: invalid argument")
		}
	}
	if len(exts) == 0 {
		return nil, fmt.Errorf("require: missing extension name")
	}
	for _, ext := range exts {
		if !supportedExtensions[ext] {
			return nil, fmt.Errorf("require: unsupported extension %q", ext)
		}
	}
	return exts, nil
}

// CheckRequires validates every top-level require of a parsed script.
func CheckRequires(s *Script) error {
	for idx := range s.Commands {
		if s.Commands[idx].Name == "require" {
			if _, err := requireExtensions(&s.Commands[idx]); err != nil {
				return err
			}
		}
	}
	return nil
}
