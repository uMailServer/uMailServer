// Package sieve implements a basic Sieve (RFC 5228) script interpreter
// for server-side mail filtering. It supports the most common actions:
// keep, discard, redirect, fileinto, reject, and stop.
package sieve

import (
	"fmt"
	"net/mail"
	"strconv"
	"strings"
)

// Action represents an action to take on a message
type Action int

const (
	ActionKeep     Action = iota // Keep in INBOX (default)
	ActionDiscard                 // Silently drop
	ActionRedirect               // Redirect to another address
	ActionFileInto                // Move to specified mailbox
	ActionReject                  // Reject with message
	ActionStop                    // Stop processing
)

// Result represents the result of Sieve script evaluation
type Result struct {
	Action     Action
	Target     string // For redirect: target address; for fileinto: mailbox name
	RejectMsg  string // For reject: rejection message
	Stop       bool   // Whether to stop processing
}

// Message represents the message being filtered
type Message struct {
	From    string
	To      []string
	Subject string
	Headers map[string][]string
	Body    string
	Size    int
}

// Script represents a parsed Sieve script
type Script struct {
	requires []string
	rules    []rule
}

type rule struct {
	conditions []condition
	actions    []action
	test       testType // for if/elsif/else structure
}

type testType int

const (
	testAny testType = iota // anyof
	testAll                 // allof
	testNot                 // not
	testSingle              // single condition
)

type condition struct {
	// Header tests
	headerName  string
	headerValue string
	headerOp    string // :is, :contains, :matches
	// Address tests
	addressPart string // :localpart, :domain, :all
	// Size tests
	sizeOver  int
	sizeUnder int
	// Exists test
	headerNames []string
	// Type
	condType condType
}

type condType int

const (
	condHeader condType = iota
	condSize
	condExists
	condAddress
	condTrue // always true
	condFalse
)

type action struct {
	actionType Action
	target     string // mailbox or address
	message    string // reject message
}

// Parse parses a Sieve script into an executable form
func Parse(script string) (*Script, error) {
	p := &parser{
		input: script,
		pos:   0,
	}
	return p.parse()
}

// Evaluate evaluates a Sieve script against a message
func (s *Script) Evaluate(msg *Message) []Result {
	var results []Result

	for _, r := range s.rules {
		matched := evaluateConditions(r, msg)
		if matched {
			for _, a := range r.actions {
				result := Result{
					Action: a.actionType,
					Target: a.target,
				}
				if a.actionType == ActionReject {
					result.RejectMsg = a.message
				}
				if a.actionType == ActionStop {
					result.Stop = true
				}
				results = append(results, result)
				if a.actionType == ActionStop {
					return results
				}
			}
		}
	}

	// Default action: keep
	if len(results) == 0 {
		results = append(results, Result{Action: ActionKeep})
	}

	return results
}

func evaluateConditions(r rule, msg *Message) bool {
	if len(r.conditions) == 0 {
		return true
	}

	switch r.test {
	case testAll:
		for _, c := range r.conditions {
			if !evaluateCondition(c, msg) {
				return false
			}
		}
		return true
	case testAny:
		for _, c := range r.conditions {
			if evaluateCondition(c, msg) {
				return true
			}
		}
		return false
	case testNot:
		if len(r.conditions) > 0 {
			return !evaluateCondition(r.conditions[0], msg)
		}
		return false
	default:
		for _, c := range r.conditions {
			if evaluateCondition(c, msg) {
				return true
			}
		}
		return false
	}
}

func evaluateCondition(c condition, msg *Message) bool {
	switch c.condType {
	case condHeader:
		return evalHeaderTest(c, msg)
	case condSize:
		return evalSizeTest(c, msg)
	case condExists:
		return evalExistsTest(c, msg)
	case condAddress:
		return evalAddressTest(c, msg)
	case condTrue:
		return true
	case condFalse:
		return false
	default:
		return false
	}
}

func evalHeaderTest(c condition, msg *Message) bool {
	values, ok := msg.Headers[c.headerName]
	if !ok {
		return false
	}

	for _, v := range values {
		if matchHeader(c.headerOp, v, c.headerValue) {
			return true
		}
	}
	return false
}

func matchHeader(op, value, pattern string) bool {
	switch op {
	case ":is", "":
		return strings.EqualFold(value, pattern)
	case ":contains":
		return strings.Contains(strings.ToLower(value), strings.ToLower(pattern))
	case ":matches":
		return matchGlob(pattern, value)
	default:
		return strings.EqualFold(value, pattern)
	}
}

func matchGlob(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		return strings.Contains(strings.ToLower(value), strings.ToLower(pattern[1:len(pattern)-1]))
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(strings.ToLower(value), strings.ToLower(pattern[1:]))
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(strings.ToLower(value), strings.ToLower(pattern[:len(pattern)-1]))
	}
	return strings.EqualFold(value, pattern)
}

func evalSizeTest(c condition, msg *Message) bool {
	if c.sizeOver > 0 {
		return msg.Size > c.sizeOver
	}
	if c.sizeUnder > 0 {
		return msg.Size < c.sizeUnder
	}
	return false
}

func evalExistsTest(c condition, msg *Message) bool {
	for _, name := range c.headerNames {
		if _, ok := msg.Headers[name]; !ok {
			return false
		}
	}
	return true
}

func evalAddressTest(c condition, msg *Message) bool {
	// Check From and To addresses
	var addresses []string
	if msg.From != "" {
		addresses = append(addresses, msg.From)
	}
	for _, to := range msg.To {
		addresses = append(addresses, to)
	}

	for _, addr := range addresses {
		parsed, err := mail.ParseAddress(addr)
		if err != nil {
			continue
		}
		email := parsed.Address
		at := strings.LastIndex(email, "@")
		if at < 0 {
			continue
		}
		localPart := email[:at]
		domain := email[at+1:]

		var compareVal string
		switch c.addressPart {
		case ":localpart":
			compareVal = localPart
		case ":domain":
			compareVal = domain
		default:
			compareVal = email
		}

		if matchHeader(c.headerOp, compareVal, c.headerValue) {
			return true
		}
	}
	return false
}

// --- Parser ---

type parser struct {
	input string
	pos   int
}

func (p *parser) parse() (*Script, error) {
	script := &Script{}

	tokens := p.tokenize()
	i := 0

	for i < len(tokens) {
		switch strings.ToLower(tokens[i]) {
		case "require":
			// Skip require statements
			i++
			if i < len(tokens) && tokens[i] == "[" {
				i++
				for i < len(tokens) && tokens[i] != "]" {
					script.requires = append(script.requires, strings.Trim(tokens[i], "\""))
					i++
				}
				i++ // skip ]
			}
		case "if":
			rule, newI, err := p.parseIf(tokens, i)
			if err != nil {
				return nil, err
			}
			script.rules = append(script.rules, *rule)
			i = newI
		default:
			// Try to parse as a standalone action
			act, newI, err := p.parseAction(tokens, i)
			if err != nil {
				i++
				continue
			}
			script.rules = append(script.rules, rule{
				conditions: []condition{{condType: condTrue}},
				actions:    []action{*act},
			})
			i = newI
		}
	}

	return script, nil
}

func (p *parser) parseIf(tokens []string, start int) (*rule, int, error) {
	i := start + 1 // skip "if"

	testType, conditions, newI, err := p.parseTest(tokens, i)
	if err != nil {
		return nil, newI, err
	}
	i = newI

	// Parse block: { actions... }
	if i < len(tokens) && tokens[i] == "{" {
		i++ // skip {
	}

	var actions []action
	for i < len(tokens) && tokens[i] != "}" {
		act, newI, err := p.parseAction(tokens, i)
		if err != nil {
			i++
			continue
		}
		actions = append(actions, *act)
		i = newI
	}
	if i < len(tokens) && tokens[i] == "}" {
		i++ // skip }
	}

	// Handle elsif/else
	for i < len(tokens) && (strings.ToLower(tokens[i]) == "elsif" || strings.ToLower(tokens[i]) == "else") {
		if strings.ToLower(tokens[i]) == "else" {
			i++ // skip "else"
			if i < len(tokens) && tokens[i] == "{" {
				i++
			}
			for i < len(tokens) && tokens[i] != "}" {
				act, newI, err := p.parseAction(tokens, i)
				if err != nil {
					i++
					continue
				}
				actions = append(actions, *act)
				i = newI
			}
			if i < len(tokens) && tokens[i] == "}" {
				i++
			}
			break
		}
		// elsif
		i++ // skip "elsif"
		_, elsifConds, newI, err := p.parseTest(tokens, i)
		if err != nil {
			return nil, newI, err
		}
		i = newI

		if i < len(tokens) && tokens[i] == "{" {
			i++
		}
		for i < len(tokens) && tokens[i] != "}" {
			act, newI, err := p.parseAction(tokens, i)
			if err != nil {
				i++
				continue
			}
			actions = append(actions, *act)
			i = newI
		}
		if i < len(tokens) && tokens[i] == "}" {
			i++
		}
		_ = elsifConds
	}

	return &rule{
		test:       testType,
		conditions: conditions,
		actions:    actions,
	}, i, nil
}

func (p *parser) parseTest(tokens []string, start int) (testType, []condition, int, error) {
	i := start

	if i >= len(tokens) {
		return testSingle, nil, i, fmt.Errorf("unexpected end of input")
	}

	tok := strings.ToLower(tokens[i])

	switch tok {
	case "allof":
		i++ // skip allof
		if i < len(tokens) && tokens[i] == "(" {
			i++
		}
		var conds []condition
		for i < len(tokens) && tokens[i] != ")" {
			_, subConds, newI, err := p.parseTest(tokens, i)
			if err != nil {
				return testSingle, nil, newI, err
			}
			conds = append(conds, subConds...)
			i = newI
			if i < len(tokens) && tokens[i] == "," {
				i++
			}
		}
		if i < len(tokens) && tokens[i] == ")" {
			i++
		}
		return testAll, conds, i, nil

	case "anyof":
		i++
		if i < len(tokens) && tokens[i] == "(" {
			i++
		}
		var conds []condition
		for i < len(tokens) && tokens[i] != ")" {
			_, subConds, newI, err := p.parseTest(tokens, i)
			if err != nil {
				return testSingle, nil, newI, err
			}
			conds = append(conds, subConds...)
			i = newI
			if i < len(tokens) && tokens[i] == "," {
				i++
			}
		}
		if i < len(tokens) && tokens[i] == ")" {
			i++
		}
		return testAny, conds, i, nil

	case "not":
		i++
		_, subConds, newI, err := p.parseTest(tokens, i)
		if err != nil {
			return testSingle, nil, newI, err
		}
		return testNot, subConds, newI, nil

	case "true":
		i++
		return testSingle, []condition{{condType: condTrue}}, i, nil

	case "false":
		i++
		return testSingle, []condition{{condType: condFalse}}, i, nil

	case "header":
		i++
		var op string
		if i < len(tokens) && strings.HasPrefix(tokens[i], ":") {
			op = tokens[i]
			i++
		}
		var headerName string
		if i < len(tokens) && tokens[i] == "[" {
			i++
			if i < len(tokens) {
				headerName = strings.Trim(tokens[i], "\"")
				i++
				for i < len(tokens) && tokens[i] != "]" {
					i++
				}
				if i < len(tokens) {
					i++ // skip ]
				}
			}
		} else if i < len(tokens) {
			headerName = strings.Trim(tokens[i], "\"")
			i++
		}
		var value string
		if i < len(tokens) {
			if tokens[i] == "[" {
				i++
				if i < len(tokens) {
					value = strings.Trim(tokens[i], "\"")
					i++
					for i < len(tokens) && tokens[i] != "]" {
						i++
					}
					if i < len(tokens) {
						i++ // skip ]
					}
				}
			} else {
				value = strings.Trim(tokens[i], "\"")
				i++
			}
		}
		cond := condition{
			condType:    condHeader,
			headerName:  headerName,
			headerOp:    op,
			headerValue: value,
		}
		return testSingle, []condition{cond}, i, nil

case "address":
		i++
		var addressPart string
		if i < len(tokens) && strings.HasPrefix(tokens[i], ":") {
			addressPart = tokens[i]
			i++
		}
		var op string
		if i < len(tokens) && strings.HasPrefix(tokens[i], ":") {
			op = tokens[i]
			i++
		}
		// Skip comparator/header-list
		if i < len(tokens) {
			i++ // header name
		}
		var value string
		if i < len(tokens) {
			value = strings.Trim(tokens[i], "\"")
			i++
		}
		cond := condition{
			condType:    condAddress,
			addressPart: addressPart,
			headerOp:    op,
			headerValue: value,
		}
		return testSingle, []condition{cond}, i, nil

	case "size":
		i++
		var sizeOver, sizeUnder int
		if i < len(tokens) {
			if tokens[i] == ":over" {
				i++
				if i < len(tokens) {
					sizeOver, _ = strconv.Atoi(tokens[i])
					i++
				}
			} else if tokens[i] == ":under" {
				i++
				if i < len(tokens) {
					sizeUnder, _ = strconv.Atoi(tokens[i])
					i++
				}
			}
		}
		cond := condition{
			condType:  condSize,
			sizeOver:  sizeOver,
			sizeUnder: sizeUnder,
		}
		return testSingle, []condition{cond}, i, nil

	case "exists":
		i++
		var names []string
		if i < len(tokens) && tokens[i] == "[" {
			i++
			for i < len(tokens) && tokens[i] != "]" {
				names = append(names, strings.Trim(tokens[i], "\""))
				i++
			}
			i++ // skip ]
		} else if i < len(tokens) {
			names = append(names, strings.Trim(tokens[i], "\""))
			i++
		}
		cond := condition{
			condType:    condExists,
			headerNames: names,
		}
		return testSingle, []condition{cond}, i, nil
	}

	return testSingle, nil, i, nil
}

func (p *parser) parseAction(tokens []string, start int) (*action, int, error) {
	i := start
	if i >= len(tokens) {
		return nil, i, fmt.Errorf("unexpected end of input")
	}

	tok := strings.ToLower(tokens[i])
	switch tok {
	case "keep":
		return &action{actionType: ActionKeep}, i + 1, nil
	case "discard":
		return &action{actionType: ActionDiscard}, i + 1, nil
	case "stop":
		return &action{actionType: ActionStop}, i + 1, nil
	case "redirect":
		i++
		if i < len(tokens) {
			target := strings.Trim(tokens[i], "\"")
			return &action{actionType: ActionRedirect, target: target}, i + 1, nil
		}
		return nil, i, fmt.Errorf("redirect: missing target")
	case "fileinto":
		i++
		if i < len(tokens) {
			mailbox := strings.Trim(tokens[i], "\"")
			return &action{actionType: ActionFileInto, target: mailbox}, i + 1, nil
		}
		return nil, i, fmt.Errorf("fileinto: missing mailbox")
	case "reject":
		i++
		if i < len(tokens) {
			msg := strings.Trim(tokens[i], "\"")
			return &action{actionType: ActionReject, message: msg}, i + 1, nil
		}
		return nil, i, fmt.Errorf("reject: missing reason")
	default:
		return nil, i + 1, fmt.Errorf("unknown action: %s", tok)
	}
}

func (p *parser) tokenize() []string {
	var tokens []string
	i := 0
	for i < len(p.input) {
		// Skip whitespace
		for i < len(p.input) && (p.input[i] == ' ' || p.input[i] == '\t' || p.input[i] == '\n' || p.input[i] == '\r') {
			i++
		}
		if i >= len(p.input) {
			break
		}

		// Skip comments
		if p.input[i] == '#' {
			for i < len(p.input) && p.input[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(p.input) && p.input[i] == '/' && p.input[i+1] == '*' {
			i += 2
			for i+1 < len(p.input) && !(p.input[i] == '*' && p.input[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}

		// Quoted string
		if p.input[i] == '"' {
			j := i + 1
			for j < len(p.input) && p.input[j] != '"' {
				if p.input[j] == '\\' {
					j++
				}
				j++
			}
			tokens = append(tokens, p.input[i:j+1])
			i = j + 1
			continue
		}

		// Special single-char tokens
		if p.input[i] == '{' || p.input[i] == '}' || p.input[i] == '[' || p.input[i] == ']' || p.input[i] == '(' || p.input[i] == ')' || p.input[i] == ',' || p.input[i] == ';' {
			tokens = append(tokens, string(p.input[i]))
			i++
			continue
		}

		// Word
		j := i
		for j < len(p.input) && p.input[j] != ' ' && p.input[j] != '\t' && p.input[j] != '\n' && p.input[j] != '\r' && p.input[j] != '{' && p.input[j] != '}' && p.input[j] != '[' && p.input[j] != ']' && p.input[j] != '(' && p.input[j] != ')' && p.input[j] != ',' && p.input[j] != ';' && p.input[j] != '"' {
			j++
		}
		if j > i {
			tokens = append(tokens, p.input[i:j])
		}
		i = j
	}

	return tokens
}
