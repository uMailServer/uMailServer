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
	Subject   string
	Body      string
	Days      int
	Addresses []string
	From      string
	Mime      bool
	Handle    string
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
	for _, cmd := range i.script.Commands {
		if cmd.Name == "require" {
			continue
		}
		actions, err := i.executeCommand(&cmd)
		if err != nil {
			return nil, err
		}
		if len(actions) > 0 {
			return actions, nil
		}
	}

	// Default: keep
	return []Action{KeepAction{}}, nil
}

func (i *Interpreter) setBuiltInVariables() {
	i.ctx.Variables["environment"] = "Sieve"
	i.ctx.Variables["spamtest"] = "0"
	i.ctx.Variables["virustest"] = "0"
}

func (i *Interpreter) processRequire(cmd *Command) error {
	for _, arg := range cmd.Arguments {
		var ext string
		switch v := arg.(type) {
		case *StringValue:
			ext = v.Value
		case *ListValue:
			for _, s := range v.Values {
				i.extensions[s] = true
				i.requireDone[s] = false
			}
			continue
		}
		i.extensions[ext] = true
		i.requireDone[ext] = false
	}
	return nil
}

func (i *Interpreter) executeCommand(cmd *Command) ([]Action, error) {
	switch cmd.Name {
	case "if", "elsif":
		return i.executeIf(cmd)
	case "require":
		return nil, nil // Already processed
	case "stop":
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
	// Parse test from arguments
	if len(cmd.Arguments) == 0 {
		return nil, nil
	}

	test, err := i.parseHeaderTest(cmd.Arguments)
	if err != nil {
		return nil, err
	}

	result, err := i.evaluateTest(test)
	if err != nil {
		return nil, err
	}

	// If command is elsif, we need previous if/elsif to be true
	if cmd.Name == "elsif" && len(i.ctx.Stack) > 0 && !i.ctx.Stack[len(i.ctx.Stack)-1] {
		// Previous conditions weren't met, skip
		return nil, nil
	}

	if cmd.Name == "elsif" {
		// Pop the previous frame since we're replacing it
		if len(i.ctx.Stack) > 0 {
			i.ctx.Stack = i.ctx.Stack[:len(i.ctx.Stack)-1]
		}
	}

	i.ctx.Stack = append(i.ctx.Stack, result)

	if result && cmd.Block != nil {
		for _, c := range cmd.Block.Commands {
			actions, err := i.executeCommand(&c)
			if err != nil {
				return nil, err
			}
			if len(actions) > 0 {
				return actions, nil
			}
		}
	}

	return nil, nil
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
	if str, ok := args[argIdx].(*StringValue); ok && str.Value == "header" {
		argIdx++
	}

	// Next arg could be a match type tag
	if argIdx < len(args) {
		if tag, ok := args[argIdx].(*TagValue); ok {
			matchType = tag.Value
			argIdx++
		}
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

	return &HeaderTest{
		Headers:   headers,
		KeyList:   keys,
		MatchType: ":" + matchType,
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
	default:
		return true, nil
	}
}

func (i *Interpreter) evaluateHeaderTest(t *HeaderTest) (bool, error) {
	// Search headers case-insensitively
	for headerKey, values := range i.ctx.Headers {
		headerKeyLower := strings.ToLower(headerKey)
		for _, headerName := range t.Headers {
			if headerKeyLower != strings.ToLower(headerName) {
				continue
			}
			value := strings.Join(values, ",")

			switch t.MatchType {
			case ":is", "is", "":
				for _, key := range t.KeyList {
					if value == key {
						return true, nil
					}
				}
			case ":contains", "contains":
				for _, key := range t.KeyList {
					// Case-insensitive contains
					if strings.Contains(strings.ToLower(value), strings.ToLower(key)) {
						return true, nil
					}
				}
			case ":matches", "matches":
				for _, key := range t.KeyList {
					pattern := strings.ReplaceAll(key, "*", ".*")
					pattern = strings.ReplaceAll(pattern, "?", ".")
					matched, err := safeRegexMatch("(?i)"+pattern, value, i.timeout)
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
		pattern := strings.ReplaceAll(value, "*", ".*")
		pattern = strings.ReplaceAll(pattern, "?", ".")
		for name, values := range i.ctx.Headers {
			if !strings.EqualFold(name, target) {
				continue
			}
			for _, v := range values {
				matched, err := safeRegexMatch("(?i)"+pattern, v, i.timeout)
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

	for _, arg := range cmd.Arguments {
		switch a := arg.(type) {
		case *TagValue:
			switch a.Value {
			case "subject":
				// Next arg is subject
			case "days":
			case "addresses":
			case "mime":
				vacation.Mime = true
			case "handle":
				// Next arg is handle
			}
		case *StringValue:
			if vacation.Subject == "" {
				vacation.Subject = a.Value
			} else if vacation.Body == "" {
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
