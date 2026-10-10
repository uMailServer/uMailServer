package sieve

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MessageContext holds the message being filtered
type MessageContext struct {
	// Envelope sender and recipients
	From    string
	To      []string
	Headers map[string][]string
	Body    []byte

	// Original message size
	Size int64

	// BodyIsFullMessage is set by callers whose Body still holds the header
	// block (the whole RFC 5322 message); the body test then skips it.
	// Without it the interpreter detects that case when Body starts with the
	// very header block Headers was parsed from.
	BodyIsFullMessage bool
}

// Action represents a Sieve action
type Action interface{}

// KeepAction keeps the message in inbox
type KeepAction struct {
	// Flags are the IMAP flags to set (RFC 5232), space separated.
	Flags string
}

// FileintoAction moves message to folder
type FileintoAction struct {
	Folder string
	// Copy (:copy, RFC 3894) files a copy and does not cancel the implicit
	// keep; Flags (:flags / internal flags, RFC 5232) are the IMAP flags to
	// set, space separated.
	Copy  bool
	Flags string
}

// RejectAction rejects the message
type RejectAction struct {
	Message string
}

// DiscardAction silently discards
type DiscardAction struct{}

// RedirectAction forwards to address
type RedirectAction struct {
	Address string
	// Copy (:copy, RFC 3894) forwards a copy; the implicit keep stays.
	Copy bool
}

// StopAction stops processing
type StopAction struct {
}

// VacationAction sends vacation auto-reply
type VacationAction struct {
	Subject    string
	Body       string
	Days       int
	Seconds    int
	SecondsSet bool
	Addresses  []string
	From       string
	Mime       bool
	Handle     string
}

// SieveContext holds execution context
type SieveContext struct {
	*MessageContext
	Variables map[string]string
	Stack     []bool // For nested tests
}

// Interpreter executes Sieve scripts
type Interpreter struct {
	script      *Script
	ctx         *SieveContext
	extensions  map[string]bool
	requireDone map[string]bool
	timeout     time.Duration // Timeout for regex matching to prevent ReDoS
	redirected  []string      // addresses already redirected this run (F5961)
	stopped     bool          // set by "stop"; ends the whole script (F5035)
	flags       []string      // imap4flags internal variable
	ops         int           // operations charged this run (maxRunOps)
	sinceClock  int
	deadline    time.Time
}

// NewInterpreter creates a new Sieve interpreter
func NewInterpreter(script *Script) *Interpreter {
	return &Interpreter{
		script:      script,
		extensions:  make(map[string]bool),
		requireDone: make(map[string]bool),
		timeout:     100 * time.Millisecond, // Default timeout for regex matching
	}
}

// regexCache caches compiled regex patterns with timeout protection
var regexCache = struct {
	sync.RWMutex
	patterns    map[string]*regexp.Regexp
	accessOrder []string // LRU tracking
	maxSize     int
}{
	patterns:    make(map[string]*regexp.Regexp),
	accessOrder: make([]string, 0, 1000),
	maxSize:     1000,
}

// safeRegexMatch matches a pattern against value with ReDoS protection
func safeRegexMatch(pattern, value string, timeout time.Duration) (bool, error) {
	// Check for obviously malicious patterns (nested quantifiers)
	if isSuspiciousPattern(pattern) {
		return false, fmt.Errorf("regex pattern too complex (potential ReDoS)")
	}
	return matchRegexp(pattern, value, timeout)
}

// globMatch matches a Sieve :matches key against value. F5241: the regexp
// built by globToRegexp only holds literals, "." and ".*", which Go's linear
// time RE2 engine evaluates without backtracking, so it must not go through
// isSuspiciousPattern (which rejected keys with four or more "*" wildcards).
func globMatch(glob, value string, timeout time.Duration) (bool, error) {
	return matchRegexp(globToRegexp(glob), value, timeout)
}

// matchRegexp compiles pattern through the LRU cache and matches it against
// value with a timeout.
func matchRegexp(pattern, value string, timeout time.Duration) (bool, error) {
	re, err := compileCached(pattern)
	if err != nil {
		return false, err
	}

	// Use context with timeout for proper cancellation
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Run regex match in goroutine with context cancellation
	resultChan := make(chan bool, 1)
	go func() {
		resultChan <- re.MatchString(value)
	}()

	select {
	case result := <-resultChan:
		return result, nil
	case <-ctx.Done():
		return false, fmt.Errorf("regex match timed out (possible ReDoS)")
	}
}

// isSuspiciousPattern checks for patterns that could cause ReDoS
func isSuspiciousPattern(pattern string) bool {
	// Check for adjacent quantifiers like ++, **, *+, +* that cause exponential backtracking
	for i := 0; i < len(pattern)-1; i++ {
		c := pattern[i]
		n := pattern[i+1]
		if (c == '+' || c == '*') && (n == '+' || n == '*') {
			return true
		}
	}

	// Check for literal substring patterns that indicate nested quantifiers
	suspicious := []string{
		"(+) ", "(+) ", "(+) ",
		"(*)",        // (a*)+ or (a*)* form
		"(+*", "(*+", // mixed quantifiers
		"++)", "*+)", "+*)", "*(+", // other nested quantifier patterns
	}
	for _, s := range suspicious {
		if strings.Contains(pattern, s) {
			return true
		}
	}

	// Also check for multiple adjacent quantifiers like .*.* without anchors
	if strings.Count(pattern, ".*") > 3 || strings.Count(pattern, ".+") > 3 {
		return true
	}

	return false
}

// Execute runs the Sieve script and returns actions. Any run-time error
// (RFC 5228 §2.10.6) is logged and returned together with the implicit keep,
// and no partial actions are returned (callers such as Manager.ProcessMessage log it), so mail is never lost.
func (i *Interpreter) Execute(msg *MessageContext) (actions []Action, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("sieve: internal error: %v", r)
		}
		if err != nil {
			actions = []Action{KeepAction{}}
		}
	}()

	i.ctx = &SieveContext{
		MessageContext: msg,
		Variables:      make(map[string]string),
	}
	i.stopped = false
	i.redirected = nil
	i.flags = nil
	i.ops, i.sinceClock = 0, 0
	i.deadline = time.Now().Add(maxRunDuration * time.Second)
	i.setBuiltInVariables()

	exts := i.script.exts
	if exts == nil {
		if exts, err = checkScript(i.script); err != nil {
			return nil, err
		}
	}
	i.extensions = exts

	all, err := i.executeCommands(i.script.Commands)
	if err != nil {
		return nil, err
	}
	all = dropCancelledDiscard(all)
	if err := checkActions(all); err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return []Action{KeepAction{}}, nil
	}
	return keepWithCopies(all), nil
}

// checkActions rejects action sets RFC 5228/5429 forbid together and bounds
// the number of actions a script may produce.
func checkActions(all []Action) error {
	if len(all) > maxActions {
		return fmt.Errorf("script produced more than %d actions", maxActions)
	}
	redirects, reject, delivers := 0, false, false
	for _, a := range all {
		switch v := a.(type) {
		case RedirectAction:
			redirects++
			delivers = true
		case FileintoAction, KeepAction:
			delivers = true
		case RejectAction:
			reject = true
			_ = v
		}
	}
	if redirects > maxRedirects {
		return fmt.Errorf("script redirects to more than %d addresses", maxRedirects)
	}
	if reject && delivers {
		return fmt.Errorf("reject conflicts with keep/fileinto/redirect")
	}
	return nil
}

// keepWithCopies restores the implicit keep when the only deliveries are
// :copy actions (RFC 3894 §3): they do not cancel it.
func keepWithCopies(all []Action) []Action {
	hasCopy := false
	for _, a := range all {
		switch v := a.(type) {
		case FileintoAction:
			if !v.Copy {
				return all
			}
			hasCopy = true
		case RedirectAction:
			if !v.Copy {
				return all
			}
			hasCopy = true
		case KeepAction, DiscardAction, RejectAction:
			return all
		}
	}
	if hasCopy {
		all = append(all, KeepAction{Flags: ""})
	}
	return all
}

// dropCancelledDiscard removes discard when the script also keeps, files or
// redirects the message: RFC 5228 §4.5 discard only cancels the implicit keep,
// it never overrides an explicit delivery action. F5035.
func dropCancelledDiscard(actions []Action) []Action {
	delivers := false
	for _, a := range actions {
		switch a.(type) {
		case KeepAction, FileintoAction, RedirectAction:
			delivers = true
		}
	}
	if !delivers {
		return actions
	}
	out := actions[:0:0]
	for _, a := range actions {
		if _, ok := a.(DiscardAction); !ok {
			out = append(out, a)
		}
	}
	return out
}

func (i *Interpreter) setBuiltInVariables() {
	i.ctx.Variables["environment"] = "Sieve"
	i.ctx.Variables["spamtest"] = "0"
	i.ctx.Variables["virustest"] = "0"
}

// executeCommands runs a command sequence. F5008: if/elsif/else form one
// chain per sequence; elsif/else run only when no earlier branch of the same
// chain was taken, and a nested block keeps its own chain state.
// F5035: actions accumulate (RFC 5228 §2.10); only "stop" ends execution.
func (i *Interpreter) executeCommands(cmds []Command) ([]Action, error) {
	var all []Action
	inChain, chainTaken := false, false
	for idx := range cmds {
		if i.stopped {
			break
		}
		cmd := &cmds[idx]
		switch cmd.Name {
		case "if":
			inChain = true
		case "elsif", "else":
			if !inChain {
				return nil, fmt.Errorf("%s without preceding if", cmd.Name)
			}
			if cmd.Name == "else" {
				inChain = false
			}
			if chainTaken {
				continue
			}
		case "require":
			inChain = false
			continue
		default:
			inChain = false
			actions, err := i.executeCommand(cmd)
			if err != nil {
				return nil, err
			}
			all = append(all, actions...)
			continue
		}

		taken, actions, err := i.executeConditional(cmd)
		if err != nil {
			return nil, err
		}
		chainTaken = taken
		all = append(all, actions...)
	}
	return all, nil
}

func (i *Interpreter) executeCommand(cmd *Command) ([]Action, error) {
	if err := i.tick(1); err != nil {
		return nil, err
	}
	switch cmd.Name {
	case "if", "elsif":
		return i.executeIf(cmd)
	case "require":
		return nil, nil // Already processed
	}
	pa, err := compileCommand(cmd, i.extensions)
	if err != nil {
		return nil, err
	}
	switch cmd.Name {
	case "stop":
		i.stopped = true
		return []Action{StopAction{}}, nil
	case "discard":
		return []Action{DiscardAction{}}, nil
	case "keep":
		return []Action{KeepAction{Flags: i.actionFlags(pa)}}, nil
	case "fileinto":
		return i.executeFileinto(cmd, pa)
	case "redirect":
		return i.executeRedirect(cmd, pa)
	case "reject":
		return []Action{RejectAction{Message: pa.pos[0].(*StringValue).Value}}, nil
	case "vacation":
		return i.executeVacation(cmd)
	case "addflag", "setflag", "removeflag":
		i.editFlags(cmd.Name, listOf(pa.pos[0]))
		return nil, nil
	}
	return nil, fmt.Errorf("unknown command %q", cmd.Name)
}

// actionFlags returns the IMAP flags of a keep/fileinto: the :flags argument
// if given, else the internal variable (RFC 5232 §3).
func (i *Interpreter) actionFlags(pa parsedArgs) string {
	if pa.has("flags") {
		return strings.Join(normFlags(listOf(pa.tags["flags"])), " ")
	}
	return strings.Join(i.flags, " ")
}

// normFlags splits space separated flag strings and drops invalid names and
// duplicates (RFC 5232 §4: invalid flags are ignored).
func normFlags(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		for _, f := range strings.Fields(s) {
			if !validFlag(f) || seen[strings.ToLower(f)] {
				continue
			}
			seen[strings.ToLower(f)] = true
			out = append(out, f)
			if len(out) >= maxFlags {
				return out
			}
		}
	}
	return out
}

func validFlag(f string) bool {
	if len(f) > 64 {
		return false
	}
	body := strings.TrimPrefix(f, `\`)
	if body == "" {
		return false
	}
	for idx := 0; idx < len(body); idx++ {
		c := body[idx]
		if c <= ' ' || c >= 0x7f || strings.IndexByte("(){%*\"]\\", c) >= 0 {
			return false
		}
	}
	return true
}

func (i *Interpreter) editFlags(op string, list []string) {
	add := normFlags(list)
	switch op {
	case "setflag":
		i.flags = add
	case "addflag":
		i.flags = normFlags(append(append([]string{}, i.flags...), add...))
	case "removeflag":
		var keep []string
		for _, f := range i.flags {
			drop := false
			for _, r := range add {
				if strings.EqualFold(f, r) {
					drop = true
				}
			}
			if !drop {
				keep = append(keep, f)
			}
		}
		i.flags = keep
	}
}

func (i *Interpreter) executeIf(cmd *Command) ([]Action, error) {
	_, actions, err := i.executeConditional(cmd)
	return actions, err
}

// executeConditional evaluates one if/elsif/else branch and runs its block
// when the branch is taken. It reports whether the branch was taken.
func (i *Interpreter) executeConditional(cmd *Command) (bool, []Action, error) {
	result := true
	if cmd.Name != "else" {
		test, err := i.parseTestCommand(cmd.Arguments)
		if err != nil {
			return false, nil, err
		}
		result, err = i.evaluateTest(test)
		if err != nil {
			return false, nil, err
		}
	}
	if !result || cmd.Block == nil {
		return result, nil, nil
	}
	actions, err := i.executeCommands(cmd.Block.Commands)
	return true, actions, err
}

// constTest is the RFC 5228 "true" / "false" test.
type constTest bool

// notTest is the RFC 5228 §5.8 "not" test. F5240.
type notTest struct{ Test }

// existsTest is the RFC 5228 §5.5 "exists" test: true when every named
// header is present. F5240.
type existsTest []string

// parseTestCommand builds the test of an if/elsif. F5006: a test that cannot
// be parsed is an error, never an implicit "true".
func (i *Interpreter) parseTestCommand(args []Value) (Test, error) {
	return buildTest(args, i.extensions, 0)
}

func (i *Interpreter) parseTest(arg Value) (Test, error) {
	if str, ok := arg.(*StringValue); ok {
		return &StringTest{Value: str.Value}, nil
	}
	if _, ok := arg.(*TagValue); ok {
		// This is a tagged test like :contains
		return nil, nil
	}
	return nil, nil
}

func (i *Interpreter) evaluateTest(test Test) (bool, error) {
	if err := i.tick(1); err != nil {
		return false, err
	}
	switch t := test.(type) {
	case *HeaderTest:
		return i.evaluateHeaderTest(t)
	case *addressTest:
		return i.evaluateAddressTest(t)
	case *envelopeTest:
		return i.evaluateEnvelopeTest(t)
	case *bodyTest:
		return i.evaluateBodyTest(t)
	case *hasflagTest:
		return i.evaluateHasflagTest(t)
	case *StringTest:
		return i.evaluateStringTest(t)
	case *SizeTest:
		return i.evaluateSizeTest(t)
	case *BooleanTest:
		return i.evaluateBooleanTest(t)
	case allOfTest:
		for _, sub := range t {
			ok, err := i.evaluateTest(sub)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	case anyOfTest:
		for _, sub := range t {
			ok, err := i.evaluateTest(sub)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case constTest:
		return bool(t), nil
	case notTest:
		result, err := i.evaluateTest(t.Test)
		if err != nil {
			return false, err
		}
		return !result, nil
	case existsTest:
		for _, name := range t {
			found := false
			for key := range i.ctx.Headers {
				if strings.EqualFold(key, name) {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, fmt.Errorf("unknown test type %T", test)
	}
}

// globToRegexp converts a Sieve :matches key (RFC 5228 §2.7.1) into an
// anchored regular expression: "*" and "?" are wildcards, "\" escapes the
// next character, and every other character is literal. F5007.
func globToRegexp(glob string) string {
	var b strings.Builder
	b.WriteString("(?s)^")
	for idx := 0; idx < len(glob); idx++ {
		switch c := glob[idx]; c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '\\':
			if idx+1 < len(glob) {
				idx++
				c = glob[idx]
			}
			b.WriteString(regexp.QuoteMeta(string(c)))
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return b.String()
}

func (i *Interpreter) evaluateHeaderTest(t *HeaderTest) (bool, error) {
	// F5010: RFC 5228 §2.7.3 default comparator is i;ascii-casemap.
	cmp, err := normComparator(t.Comparator)
	if err != nil {
		return false, err
	}
	ms := matchSpec{Type: strings.TrimPrefix(t.MatchType, ":"), Relation: t.Relation, Comparator: cmp}
	if ms.Type == "" {
		ms.Type = "is"
	}
	return i.matchValues(ms, i.headerValues(t.Headers), t.KeyList)
}

func (i *Interpreter) evaluateStringTest(t *StringTest) (bool, error) {
	value := t.Value
	target := t.Target

	switch t.MatchType {
	case ":is", "is", "":
		// Check if header values match exactly (case-sensitive)
		for name, values := range i.ctx.Headers {
			// Check if header name matches target (case-insensitive)
			if !strings.EqualFold(name, target) {
				continue
			}
			for _, v := range values {
				if v == value {
					return true, nil
				}
			}
		}
	case ":contains", "contains":
		// Check if header values contain substring
		for name, values := range i.ctx.Headers {
			if !strings.EqualFold(name, target) {
				continue
			}
			for _, v := range values {
				if strings.Contains(v, value) {
					return true, nil
				}
			}
		}
	case ":matches", "matches":
		for name, values := range i.ctx.Headers {
			if !strings.EqualFold(name, target) {
				continue
			}
			for _, v := range values {
				matched, err := matchRegexp("(?i)"+globToRegexp(value), v, i.timeout)
				if err != nil {
					return false, nil
				}
				if matched {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (i *Interpreter) evaluateSizeTest(t *SizeTest) (bool, error) {
	switch t.Relation {
	case ":over", "over":
		return i.ctx.Size > t.Size, nil
	case ":under", "under":
		return i.ctx.Size < t.Size, nil
	}
	return false, nil
}

func (i *Interpreter) evaluateBooleanTest(t *BooleanTest) (bool, error) {
	if len(t.Tests) == 0 {
		return true, nil
	}
	for _, test := range t.Tests {
		result, err := i.evaluateTest(test)
		if err != nil {
			return false, err
		}
		if result {
			return true, nil
		}
	}
	return false, nil
}

func (i *Interpreter) executeFileinto(cmd *Command, pa parsedArgs) ([]Action, error) {
	folder := pa.pos[0].(*StringValue).Value
	if folder == "" {
		return nil, nil
	}

	// F5960: RFC 5228 §4.1 lets an implementation fall back to the implicit
	// keep when a mailbox name cannot be used. Names with control characters
	// (including NUL, CR, LF) or ".." segments would otherwise be created
	// verbatim as mailboxes by the delivery layer.
	if !validFileintoFolder(folder) {
		return []Action{KeepAction{Flags: i.actionFlags(pa)}}, nil
	}

	return []Action{FileintoAction{Folder: folder, Copy: pa.has("copy"), Flags: i.actionFlags(pa)}}, nil
}

// validFileintoFolder reports whether a fileinto target is a usable mailbox
// name: valid UTF-8, no control characters, not absolute and without "." or
// ".." path segments. F5960.
func validFileintoFolder(name string) bool {
	if !utf8.ValidString(name) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, seg := range strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." || seg == "." {
			return false
		}
	}
	return true
}

func (i *Interpreter) executeRedirect(cmd *Command, pa parsedArgs) ([]Action, error) {
	address := pa.pos[0].(*StringValue).Value
	if address == "" {
		return nil, nil
	}

	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return nil, fmt.Errorf("invalid redirect address: %s", address)
	}
	// F5961: use the bare addr-spec ("Bob <b@x>" is not a valid RCPT TO) and
	// do not redirect twice to the same address (RFC 5228 §4.2).
	address = parsed.Address
	for _, prev := range i.redirected {
		if strings.EqualFold(prev, address) {
			return nil, nil
		}
	}
	i.redirected = append(i.redirected, address)

	return []Action{RedirectAction{Address: address, Copy: pa.has("copy")}}, nil
}

func (i *Interpreter) executeVacation(cmd *Command) ([]Action, error) {
	vacation := VacationAction{
		Days: 7, // Default interval
	}

	// F5242: RFC 5230 §4: vacation [":days" number] [":subject" string]
	// [":from" string] [":addresses" string-list] [":mime"] [":handle" string]
	// <reason: string>. Tags may come in any order; the positional string is
	// the reason, i.e. the reply body. The parser stores the first tag in
	// cmd.Tag, so it is put back in front of the arguments.
	args := cmd.Arguments
	if cmd.Tag != "" {
		args = append([]Value{&TagValue{Value: cmd.Tag}}, cmd.Arguments...)
	}
	nextString := func(idx int) (string, bool) {
		if idx < len(args) {
			if sv, ok := args[idx].(*StringValue); ok {
				return sv.Value, true
			}
		}
		return "", false
	}
	nextNumber := func(idx int) (int, bool) {
		if idx < len(args) {
			if nv, ok := args[idx].(*NumberValue); ok {
				return int(nv.Value), true
			}
		}
		return 0, false
	}

	for argIdx := 0; argIdx < len(args); argIdx++ {
		switch a := args[argIdx].(type) {
		case *TagValue:
			switch strings.ToLower(a.Value) {
			case "subject":
				if v, ok := nextString(argIdx + 1); ok {
					vacation.Subject = v
					argIdx++
				}
			case "from":
				if v, ok := nextString(argIdx + 1); ok {
					vacation.From = v
					argIdx++
				}
			case "handle":
				if v, ok := nextString(argIdx + 1); ok {
					vacation.Handle = v
					argIdx++
				}
			case "addresses":
				if argIdx+1 < len(args) {
					switch v := args[argIdx+1].(type) {
					case *ListValue:
						vacation.Addresses = v.Values
						argIdx++
					case *StringValue:
						vacation.Addresses = []string{v.Value}
						argIdx++
					}
				}
			case "days":
				if v, ok := nextNumber(argIdx + 1); ok {
					vacation.Days = v
					argIdx++
				}
			case "seconds":
				vacation.SecondsSet = true
				if v, ok := nextNumber(argIdx + 1); ok {
					vacation.Seconds = v
					argIdx++
				}
			case "mime":
				vacation.Mime = true
			}
		case *StringValue:
			// The reason is the reply body.
			if vacation.Body == "" {
				vacation.Body = a.Value
			}
		case *NumberValue:
			vacation.Days = int(a.Value)
		}
	}

	// F5962: RFC 5230 §4.1 :days is a positive number; 0 (or less) would
	// disable the reply suppression window and answer every message.
	if vacation.Days < 1 && !vacation.SecondsSet {
		vacation.Days = 1
	}

	// Only send vacation if enabled
	if vacation.Subject == "" && vacation.Body == "" {
		return nil, nil
	}

	return []Action{vacation}, nil
}

// ExecuteScript is a convenience function
func ExecuteScript(script string, msg *MessageContext) ([]Action, error) {
	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		return nil, err
	}
	interp := NewInterpreter(s)
	return interp.Execute(msg)
}
