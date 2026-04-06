// Package sieve implements RFC 5228 - Sieve: An Email Filtering Language
package sieve

import (
	"fmt"
	"strings"
)

// Token represents a lexical token in a Sieve script
type Token int

const (
	TokInvalid Token = iota
	TokIdentifier
	TokString
	TokNumber
	TokTag
	TokLeftBrace
	TokRightBrace
	TokLeftParen
	TokRightParen
	TokLeftBracket
	TokRightBracket
	TokSemicolon
	TokComma
	TokColon
	TokWhitespace
	TokComment
	TokEOL
	TokEOF
)

// TokenData holds token value
type TokenData struct {
	Token    Token
	Value    string
	Position int
}

// Parser parses Sieve scripts into an AST
type Parser struct {
	input    string
	pos      int
	length   int
	tokens   []TokenData
}

// AST node types
type Node interface {
	nodeType() string
}

type Script struct {
	Commands []Command
}

type Command struct {
	Name     string
	Tag      string
	Arguments []Value
	Block    *Block
}

type Block struct {
	Commands []Command
}

// Value represents a Sieve value
type Value interface{}

// StringValue is a quoted or literal string
type StringValue struct {
	Value   string
	IsLiteral bool
}

// NumberValue is an integer
type NumberValue struct {
	Value int64
}

// TagValue is a tagged argument like :contains
type TagValue struct {
	Value string
}

// ListValue is a string list
type ListValue struct {
	Values []string
}

// Test represents a Sieve test
type Test interface{}

// TestCommand is a command used as a test (if, elsif)
type TestCommand struct {
	Test    Test
	Block   *Block
}

// HeaderTest represents the header test
type HeaderTest struct {
	Headers     []string
	KeyList     []string
	MatchType   string // :is, :contains, :matches
	Comparator  string
}

// EnvelopeTest represents the envelope test
type EnvelopeTest struct {
	EnvelopePart string // from, to, etc.
	MatchType   string
	KeyList     []string
}

// SizeTest represents the size test
type SizeTest struct {
	Relation string // :over, :under
	Size     int64
}

// BooleanTest wraps another test
type BooleanTest struct {
	Tests []Test
}

// StringTest is a simple string comparison test
type StringTest struct {
	Variables bool
	MatchType string
	Target    string
	Value     string
}

// NewParser creates a new Sieve parser
func NewParser(script string) *Parser {
	return &Parser{
		input:  script,
		pos:    0,
		length: len(script),
	}
}

// Parse parses a Sieve script and returns the AST
func (p *Parser) Parse() (*Script, error) {
	script := &Script{Commands: []Command{}}

	for p.pos < p.length {
		// Skip whitespace and comments
		p.skipWhitespaceAndComments()
		if p.pos >= p.length {
			break
		}

		cmd, err := p.parseCommand()
		if err != nil {
			return nil, fmt.Errorf("parse error at position %d: %w", p.pos, err)
		}
		if cmd != nil {
			script.Commands = append(script.Commands, *cmd)
		}
	}

	return script, nil
}

func (p *Parser) skipWhitespaceAndComments() {
	for p.pos < p.length {
		ch := p.input[p.pos]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			p.pos++
		} else if ch == '#' {
			// Single-line comment
			for p.pos < p.length && p.input[p.pos] != '\n' {
				p.pos++
			}
		} else if ch == '/' && p.pos+1 < p.length && p.input[p.pos+1] == '*' {
			// Multi-line comment
			p.pos += 2
			for p.pos+1 < p.length {
				if p.input[p.pos] == '*' && p.input[p.pos+1] == '/' {
					p.pos += 2
					break
				}
				p.pos++
			}
		} else {
			break
		}
	}
}

func (p *Parser) parseCommand() (*Command, error) {
	p.skipWhitespaceAndComments()
	if p.pos >= p.length {
		return nil, nil
	}

	// Check for identifier
	if !isAlpha(p.input[p.pos]) {
		return nil, fmt.Errorf("expected identifier at position %d", p.pos)
	}

	cmd := &Command{}

	// Parse command name
	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}
	cmd.Name = strings.ToLower(name)

	// Parse optional tag
	p.skipWhitespaceAndComments()
	if p.pos < p.length && p.input[p.pos] == ':' {
		tag, err := p.parseTag()
		if err != nil {
			return nil, err
		}
		cmd.Tag = tag
		p.skipWhitespaceAndComments()
	}

	// Parse arguments
	for p.pos < p.length && !p.isCommandTerminator() {
		arg, err := p.parseArgument()
		if err != nil {
			return nil, err
		}
		if arg != nil {
			cmd.Arguments = append(cmd.Arguments, arg)
		}
		p.skipWhitespaceAndComments()
	}

	// Parse block (if present)
	if p.pos < p.length && p.input[p.pos] == '{' {
		block, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		cmd.Block = block
	}

	// Consume semicolon if present
	if p.pos < p.length && p.input[p.pos] == ';' {
		p.pos++
	}

	return cmd, nil
}

func (p *Parser) isCommandTerminator() bool {
	return p.input[p.pos] == ';' || p.input[p.pos] == '{' || p.pos >= p.length
}

func (p *Parser) parseBlock() (*Block, error) {
	if p.pos >= p.length || p.input[p.pos] != '{' {
		return nil, fmt.Errorf("expected '{' at position %d", p.pos)
	}
	p.pos++ // skip '{'

	block := &Block{Commands: []Command{}}

	for p.pos < p.length && p.input[p.pos] != '}' {
		p.skipWhitespaceAndComments()
		if p.pos >= p.length || p.input[p.pos] == '}' {
			break
		}

		cmd, err := p.parseCommand()
		if err != nil {
			return nil, err
		}
		if cmd != nil {
			block.Commands = append(block.Commands, *cmd)
		}
	}

	if p.pos >= p.length {
		return nil, fmt.Errorf("unclosed block")
	}
	p.pos++ // skip '}'

	return block, nil
}

func (p *Parser) parseIdentifier() (string, error) {
	start := p.pos
	for p.pos < p.length && isAlnumUnderscore(p.input[p.pos]) {
		p.pos++
	}
	if start == p.pos {
		return "", fmt.Errorf("expected identifier at position %d", p.pos)
	}
	return p.input[start:p.pos], nil
}

func (p *Parser) parseTag() (string, error) {
	if p.pos >= p.length || p.input[p.pos] != ':' {
		return "", fmt.Errorf("expected ':' at position %d", p.pos)
	}
	p.pos++
	start := p.pos
	for p.pos < p.length && isAlnumUnderscore(p.input[p.pos]) {
		p.pos++
	}
	if start == p.pos {
		return "", fmt.Errorf("expected tag name after ':' at position %d", p.pos)
	}
	return p.input[start:p.pos], nil
}

func (p *Parser) parseArgument() (Value, error) {
	p.skipWhitespaceAndComments()
	if p.pos >= p.length {
		return nil, nil
	}

	ch := p.input[p.pos]

	// Tag argument
	if ch == ':' {
		tag, err := p.parseTag()
		if err != nil {
			return nil, err
		}
		return &TagValue{Value: tag}, nil
	}

	// String argument
	if ch == '"' {
		return p.parseString()
	}

	// Literal string (multiline)
	if ch == '[' {
		return p.parseStringList()
	}

	// Number
	if isDigit(ch) || (ch == '-' && p.pos+1 < p.length && isDigit(p.input[p.pos+1])) {
		return p.parseNumber()
	}

	// Identifier (could be string or test)
	start := p.pos
	for p.pos < p.length && isAlnumUnderscore(p.input[p.pos]) {
		p.pos++
	}
	if start == p.pos {
		return nil, nil
	}

	return &StringValue{Value: p.input[start:p.pos]}, nil
}

func (p *Parser) parseString() (*StringValue, error) {
	if p.pos >= p.length || p.input[p.pos] != '"' {
		return nil, fmt.Errorf("expected '\"' at position %d", p.pos)
	}
	p.pos++ // skip opening quote

	var builder strings.Builder
	for p.pos < p.length {
		ch := p.input[p.pos]
		if ch == '\\' && p.pos+1 < p.length {
			// Escaped character
			p.pos++
			switch p.input[p.pos] {
			case 'n':
				builder.WriteByte('\n')
			case 'r':
				builder.WriteByte('\r')
			case 't':
				builder.WriteByte('\t')
			case '\\':
				builder.WriteByte('\\')
			case '"':
				builder.WriteByte('"')
			default:
				builder.WriteByte(p.input[p.pos])
			}
			p.pos++
		} else if ch == '"' {
			p.pos++ // skip closing quote
			break
		} else {
			builder.WriteByte(ch)
			p.pos++
		}
	}

	return &StringValue{Value: builder.String()}, nil
}

func (p *Parser) parseStringList() (*ListValue, error) {
	if p.pos >= p.length || p.input[p.pos] != '[' {
		return nil, fmt.Errorf("expected '[' at position %d", p.pos)
	}
	p.pos++ // skip '['

	list := &ListValue{Values: []string{}}

	for p.pos < p.length && p.input[p.pos] != ']' {
		p.skipWhitespaceAndComments()
		if p.input[p.pos] == '"' {
			str, err := p.parseString()
			if err != nil {
				return nil, err
			}
			list.Values = append(list.Values, str.Value)
		} else {
			// Bare word
			start := p.pos
			for p.pos < p.length && !isWhitespace(p.input[p.pos]) && p.input[p.pos] != ',' && p.input[p.pos] != ']' {
				p.pos++
			}
			list.Values = append(list.Values, p.input[start:p.pos])
		}
		p.skipWhitespaceAndComments()
		if p.pos < p.length && p.input[p.pos] == ',' {
			p.pos++
		}
	}

	if p.pos >= p.length {
		return nil, fmt.Errorf("unclosed string list")
	}
	p.pos++ // skip ']'

	return list, nil
}

func (p *Parser) parseNumber() (*NumberValue, error) {
	start := p.pos

	if p.pos < p.length && p.input[p.pos] == '-' {
		p.pos++
	}

	for p.pos < p.length && isDigit(p.input[p.pos]) {
		p.pos++
	}

	value := p.input[start:p.pos]
	var n int64
	fmt.Sscanf(value, "%d", &n)

	return &NumberValue{Value: n}, nil
}

func (p *Parser) lookahead() Token {
	savedPos := p.pos
	defer func() { p.pos = savedPos }()

	p.skipWhitespaceAndComments()
	if p.pos >= p.length {
		return TokEOF
	}

	ch := p.input[p.pos]
	if ch == '{' {
		return TokLeftBrace
	}
	if ch == '}' {
		return TokRightBrace
	}
	if ch == ';' {
		return TokSemicolon
	}
	if ch == ':' {
		return TokTag
	}

	return TokIdentifier
}

// Helper functions
func isAlpha(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isAlnum(ch byte) bool {
	return isAlpha(ch) || isDigit(ch)
}

func isAlnumUnderscore(ch byte) bool {
	return isAlnum(ch) || ch == '_'
}

func isWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n'
}

// MustCompile is a convenience function to parse and compile a Sieve script
func MustCompile(script string) *Script {
	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		panic(err)
	}
	return s
}

// String returns a string representation of the script
func (s *Script) String() string {
	var sb strings.Builder
	for _, cmd := range s.Commands {
		sb.WriteString(cmd.Name)
		if cmd.Tag != "" {
			sb.WriteString(" :")
			sb.WriteString(cmd.Tag)
		}
		for _, arg := range cmd.Arguments {
			sb.WriteString(" ")
			sb.WriteString(fmt.Sprintf("%v", arg))
		}
		if cmd.Block != nil {
			sb.WriteString(" { ")
			for _, c := range cmd.Block.Commands {
				sb.WriteString(c.Name)
				sb.WriteString("; ")
			}
			sb.WriteString("} ")
		}
		sb.WriteString("; ")
	}
	return sb.String()
}
