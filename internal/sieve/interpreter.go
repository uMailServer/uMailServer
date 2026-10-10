package sieve

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"
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
}

// Action represents a Sieve action
type Action interface{}

// KeepAction keeps the message in inbox
type KeepAction struct{}

// FileintoAction moves message to folder
type FileintoAction struct {
	Folder string
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
	stopped     bool          // set by "stop"; ends the whole script (F5035)
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
	// Try to get from cache first with proper locking
	regexCache.Lock()
	re, ok := regexCache.patterns[pattern]
	if ok {
		regexCache.Unlock()
	} else {
		// Validate and compile
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			regexCache.Unlock()
			return false, fmt.Errorf("invalid regex pattern: %w", err)
		}

		// LRU eviction if at capacity
		if len(regexCache.patterns) >= regexCache.maxSize {
			// Remove oldest 25% (250 entries)
			removeCount := regexCache.maxSize / 4
			for i := 0; i < removeCount && len(regexCache.accessOrder) > 0; i++ {
				oldest := regexCache.accessOrder[0]
				regexCache.accessOrder = regexCache.accessOrder[1:]
				delete(regexCache.patterns, oldest)
			}
		}

		regexCache.patterns[pattern] = re
		regexCache.accessOrder = append(regexCache.accessOrder, pattern)
		regexCache.Unlock()
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

// Execute runs the Sieve script and returns actions
func (i *Interpreter) Execute(msg *MessageContext) ([]Action, error) {
	i.ctx = &SieveContext{
		MessageContext: msg,
		Variables:      make(map[string]string),
	}

	i.stopped = false

	// Set built-in variables
	i.setBuiltInVariables()

	// Process require statements first
	for _, cmd := range i.script.Commands {
		if cmd.Name == "require" {
			if err := i.processRequire(&cmd); err != nil {
				return nil, err
			}
		}
	}

	// Execute commands
	actions, err := i.executeCommands(i.script.Commands)
	if err != nil {
		return nil, err
	}
	actions = dropCancelledDiscard(actions)
	if len(actions) > 0 {
		return actions, nil
	}

	// Default: keep
	return []Action{KeepAction{}}, nil
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

// supportedExtensions lists the capabilities this interpreter implements.
// RFC 5228 §3.2: requiring anything else must fail the script. F5036.
// F5343: "variables" (RFC 5229) is not listed: ${name} expansion and the
// string test are not implemented, so requiring it must fail the script.
var supportedExtensions = map[string]bool{
	"fileinto":                   true,
	"reject":                     true,
	"vacation":                   true,
	"vacation-seconds":           true,
	"comparator-i;octet":         true,
	"comparator-i;ascii-casemap": true,
}

// requireExtensions returns the extension names named by a require command,
// or an error if any of them is not supported.
func requireExtensions(cmd *Command) ([]string, error) {
	var exts []string
	for _, arg := range cmd.Arguments {
		switch v := arg.(type) {
		case *StringValue:
			exts = append(exts, v.Value)
		case *ListValue:
			exts = append(exts, v.Values...)
		default:
			return nil, fmt.Errorf("require: invalid argument")
		}
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

func (i *Interpreter) processRequire(cmd *Command) error {
	exts, err := requireExtensions(cmd)
	if err != nil {
		return err
	}
	for _, ext := range exts {
		i.extensions[ext] = true
		i.requireDone[ext] = false
	}
	return nil
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
	switch cmd.Name {
	case "if", "elsif":
		return i.executeIf(cmd)
	case "require":
		return nil, nil // Already processed
	case "stop":
		i.stopped = true
		return []Action{StopAction{}}, nil
	case "discard":
		return []Action{DiscardAction{}}, nil
	case "keep":
		return []Action{KeepAction{}}, nil
	case "fileinto":
		return i.executeFileinto(cmd)
	case "redirect":
		return i.executeRedirect(cmd)
	case "reject":
		return i.executeReject(cmd)
	case "vacation":
		return i.executeVacation(cmd)
	case "set":
		return i.executeSet(cmd)
	case "addheader":
		return i.executeAddHeader(cmd)
	case "deleteheader":
		return i.executeDeleteHeader(cmd)
	default:
		return nil, nil // Unknown command, ignore
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
	if len(args) > 0 {
		if sv, ok := args[0].(*StringValue); ok {
			switch strings.ToLower(sv.Value) {
			case "true", "false":
				if len(args) != 1 {
					return nil, fmt.Errorf("%s test takes no arguments", sv.Value)
				}
				return constTest(strings.EqualFold(sv.Value, "true")), nil
			case "not":
				// F5240: RFC 5228 §5.8.
				inner, err := i.parseTestCommand(args[1:])
				if err != nil {
					return nil, err
				}
				return notTest{inner}, nil
			case "exists":
				// F5240: RFC 5228 §5.5.
				if len(args) == 2 {
					switch v := args[1].(type) {
					case *StringValue:
						return existsTest{v.Value}, nil
					case *ListValue:
						return existsTest(v.Values), nil
					}
				}
				return nil, fmt.Errorf("malformed exists test")
			case "header":
			case "size":
				if len(args) == 3 {
					tag, okTag := args[1].(*TagValue)
					num, okNum := args[2].(*NumberValue)
					if okTag && okNum && (strings.EqualFold(tag.Value, "over") || strings.EqualFold(tag.Value, "under")) {
						return &SizeTest{Relation: ":" + strings.ToLower(tag.Value), Size: num.Value}, nil
					}
				}
				return nil, fmt.Errorf("malformed size test")
			default:
				// F5240: an unimplemented test (address, envelope, ...) must not
				// be evaluated as a header test on a header named after it.
				return nil, fmt.Errorf("unsupported test %q", sv.Value)
			}
		}
	}
	test, err := i.parseHeaderTest(args)
	if err != nil {
		return nil, err
	}
	if test == nil {
		return nil, fmt.Errorf("unsupported or malformed test")
	}
	return test, nil
}

// parseHeaderTest parses a header test from command arguments
// Format: header [:contains|:is|:matches] <header-names> <key-list>
func (i *Interpreter) parseHeaderTest(args []Value) (Test, error) {
	if len(args) < 2 {
		return nil, nil
	}

	var matchType string
	var headers []string
	var keys []string

	argIdx := 0

	// First arg could be "header" string or a tag like :contains
	// F5342: identifiers are case-insensitive (RFC 5228 §2.1).
	if str, ok := args[argIdx].(*StringValue); ok && strings.EqualFold(str.Value, "header") {
		argIdx++
	}

	// Next args may be match-type and :comparator tags, in any order.
	var comparator string
	for argIdx < len(args) {
		tag, ok := args[argIdx].(*TagValue)
		if !ok {
			break
		}
		argIdx++
		if strings.EqualFold(tag.Value, "comparator") {
			var sv *StringValue
			if argIdx < len(args) {
				sv, _ = args[argIdx].(*StringValue)
			}
			if sv == nil {
				return nil, fmt.Errorf(":comparator requires a string argument")
			}
			comparator = sv.Value
			argIdx++
			continue
		}
		matchType = tag.Value
	}

	// Next arg(s) are header names
	for argIdx < len(args) {
		switch arg := args[argIdx].(type) {
		case *StringValue:
			if len(headers) == 0 {
				headers = []string{arg.Value}
			} else {
				keys = append(keys, arg.Value)
			}
			argIdx++
		case *ListValue:
			if len(headers) == 0 {
				headers = arg.Values
			} else {
				keys = append(keys, arg.Values...)
			}
			argIdx++
		default:
			argIdx++
		}
	}

	if len(headers) == 0 || len(keys) == 0 {
		return nil, nil
	}

	// F5243: RFC 5228 §2.7.1 default match type is :is.
	if matchType == "" {
		matchType = "is"
	}

	return &HeaderTest{
		Headers:    headers,
		KeyList:    keys,
		MatchType:  ":" + matchType,
		Comparator: comparator,
	}, nil
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
	switch t := test.(type) {
	case *HeaderTest:
		return i.evaluateHeaderTest(t)
	case *StringTest:
		return i.evaluateStringTest(t)
	case *SizeTest:
		return i.evaluateSizeTest(t)
	case *BooleanTest:
		return i.evaluateBooleanTest(t)
	case constTest:
		return bool(t), nil
	case notTest:
		result, err := i.evaluateTest(t.Test)
		return !result, err
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
		return true, nil
	}
}

// asciiLower folds only ASCII letters, as the i;ascii-casemap comparator
// (RFC 4790 §9.2) requires.
func asciiLower(s string) string {
	b := []byte(s)
	for idx, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[idx] = c + ('a' - 'A')
		}
	}
	return string(b)
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
	fold := true
	switch t.Comparator {
	case "", "i;ascii-casemap":
	case "i;octet":
		fold = false
	default:
		return false, fmt.Errorf("unsupported comparator %q", t.Comparator)
	}
	cmp := func(s string) string {
		if fold {
			return asciiLower(s)
		}
		return s
	}

	// Search headers case-insensitively
	for headerKey, values := range i.ctx.Headers {
		headerKeyLower := strings.ToLower(headerKey)
		for _, headerName := range t.Headers {
			if headerKeyLower != strings.ToLower(headerName) {
				continue
			}
			for _, rawValue := range values {
				value := cmp(rawValue)
				switch t.MatchType {
				case ":is", "is", "":
					for _, key := range t.KeyList {
						if value == cmp(key) {
							return true, nil
						}
					}
				case ":contains", "contains":
					for _, key := range t.KeyList {
						if strings.Contains(value, cmp(key)) {
							return true, nil
						}
					}
				case ":matches", "matches":
					for _, key := range t.KeyList {
						matched, err := globMatch(cmp(key), value, i.timeout)
						if err != nil {
							// Log the error but don't fail the entire filter
							// Just return false for this test
							return false, nil
						}
						if matched {
							return true, nil
						}
					}
				}
			}
		}
	}
	return false, nil
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

func (i *Interpreter) executeFileinto(cmd *Command) ([]Action, error) {
	if len(cmd.Arguments) == 0 {
		return nil, nil
	}

	var folder string
	switch arg := cmd.Arguments[0].(type) {
	case *StringValue:
		folder = arg.Value
	case *TagValue:
		// Tagged argument like :create
		if len(cmd.Arguments) > 1 {
			if str, ok := cmd.Arguments[1].(*StringValue); ok {
				folder = str.Value
			}
		}
	}

	if folder == "" {
		return nil, nil
	}

	return []Action{FileintoAction{Folder: folder}}, nil
}

func (i *Interpreter) executeRedirect(cmd *Command) ([]Action, error) {
	if len(cmd.Arguments) == 0 {
		return nil, nil
	}

	var address string
	switch arg := cmd.Arguments[0].(type) {
	case *StringValue:
		address = arg.Value
	}

	if address == "" {
		return nil, nil
	}

	// Validate email address
	if _, err := mail.ParseAddress(address); err != nil {
		return nil, fmt.Errorf("invalid redirect address: %s", address)
	}

	return []Action{RedirectAction{Address: address}}, nil
}

func (i *Interpreter) executeReject(cmd *Command) ([]Action, error) {
	if len(cmd.Arguments) == 0 {
		return []Action{DiscardAction{}}, nil
	}

	var message string
	switch arg := cmd.Arguments[0].(type) {
	case *StringValue:
		message = arg.Value
	}

	return []Action{RejectAction{Message: message}}, nil
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

	// Only send vacation if enabled
	if vacation.Subject == "" && vacation.Body == "" {
		return nil, nil
	}

	return []Action{vacation}, nil
}

func (i *Interpreter) executeSet(cmd *Command) ([]Action, error) {
	if len(cmd.Arguments) < 2 {
		return nil, nil
	}

	var name, value string
	if tag, ok := cmd.Arguments[0].(*TagValue); ok {
		name = tag.Value
	}
	if str, ok := cmd.Arguments[1].(*StringValue); ok {
		value = str.Value
	}

	if name != "" {
		i.ctx.Variables[name] = value
	}

	return nil, nil
}

func (i *Interpreter) executeAddHeader(cmd *Command) ([]Action, error) {
	// Add header to message - would need to modify message in pipeline
	return nil, nil
}

func (i *Interpreter) executeDeleteHeader(cmd *Command) ([]Action, error) {
	// Delete header from message - would need to modify message in pipeline
	return nil, nil
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
