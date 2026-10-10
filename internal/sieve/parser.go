// Package sieve implements RFC 5228 - Sieve: An Email Filtering Language
package sieve

import (
	"fmt"
	"math"
	"strconv"
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
	input  string
	pos    int
	length int
}

// AST node types
type Node interface {
	nodeType() string
}

type Script struct {
	Commands []Command
}

type Command struct {
	Name      string
	Tag       string
	Arguments []Value
	Block     *Block
}

type Block struct {
	Commands []Command
}

// Value represents a Sieve value
type Value interface{}

// StringValue is a quoted or literal string
type StringValue struct {
	Value     string
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
	Test  Test
	Block *Block
}

// HeaderTest represents the header test
type HeaderTest struct {
	Headers    []string
	KeyList    []string
	MatchType  string // :is, :contains, :matches
	Comparator string
}

// EnvelopeTest represents the envelope test
type EnvelopeTest struct {
	EnvelopePart string // from, to, etc.
	MatchType    string
	KeyList      []string
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
		bare := isAlpha(p.input[p.pos])
		arg, err := p.parseArgument()
		if err != nil {
			return nil, err
		}
		// F5340: RFC 5228 §8.2 arguments = *argument [test / test-list]; an
		// identifier argument is a test, which only if/elsif take. Anywhere
		// else it is a missing ';' (`fileinto "A" keep;`), not an argument.
		if sv, ok := arg.(*StringValue); ok && bare && !sv.IsLiteral && cmd.Name != "if" && cmd.Name != "elsif" {
			return nil, fmt.Errorf("unexpected identifier %q in %s command (missing ';'?)", sv.Value, cmd.Name)
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
	} else if cmd.Block == nil {
		// F5340: RFC 5228 §8.2 command = identifier arguments (";" / block).
		return nil, fmt.Errorf("expected ';' after %s command", cmd.Name)
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
		// F5005: returning (nil, nil) here leaves p.pos unchanged, so the
		// caller's argument loop never progresses and Parse spins forever.
		return nil, fmt.Errorf("unexpected character %q at position %d", ch, p.pos)
	}

	word := p.input[start:p.pos]
	if strings.EqualFold(word, "text") && p.pos < p.length && p.input[p.pos] == ':' {
		return p.parseMultiline()
	}
	return &StringValue{Value: word}, nil
}

func (p *Parser) parseString() (*StringValue, error) {
	if p.pos >= p.length || p.input[p.pos] != '"' {
		return nil, fmt.Errorf("expected '\"' at position %d", p.pos)
	}
	p.pos++ // skip opening quote

	// F5038: RFC 5228 §2.4.2 defines only \\ and \" ; any other escaped
	// character stands for itself ("\n" is "n"). A string must be closed.
	var builder strings.Builder
	for p.pos < p.length {
		ch := p.input[p.pos]
		switch {
		case ch == '\\' && p.pos+1 < p.length:
			p.pos++
			builder.WriteByte(p.input[p.pos])
			p.pos++
		case ch == '"':
			p.pos++ // skip closing quote
			return &StringValue{Value: builder.String()}, nil
		default:
			builder.WriteByte(ch)
			p.pos++
		}
	}

	return nil, fmt.Errorf("unterminated quoted string")
}

// parseMultiline parses the body of an RFC 5228 §2.4.2 multi-line string;
// p.pos is at the ':' after "text". F5039. The string ends at a line holding
// only "."; a leading ".." is dot-unstuffed to ".". Line endings are kept.
func (p *Parser) parseMultiline() (*StringValue, error) {
	p.pos++ // skip ':'
	for p.pos < p.length && (p.input[p.pos] == ' ' || p.input[p.pos] == '\t') {
		p.pos++
	}
	if p.pos < p.length && p.input[p.pos] == '#' {
		for p.pos < p.length && p.input[p.pos] != '\n' {
			p.pos++
		}
	} else if p.pos < p.length && p.input[p.pos] == '\r' {
		p.pos++
	}
	if p.pos >= p.length || p.input[p.pos] != '\n' {
		return nil, fmt.Errorf("expected end of line after text: at position %d", p.pos)
	}
	p.pos++

	var builder strings.Builder
	for p.pos < p.length {
		end := strings.IndexByte(p.input[p.pos:], '\n')
		if end < 0 {
			break
		}
		line := p.input[p.pos : p.pos+end+1]
		p.pos += end + 1
		if strings.TrimRight(line, "\r\n") == "." {
			return &StringValue{Value: builder.String(), IsLiteral: true}, nil
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		builder.WriteString(line)
	}
	return nil, fmt.Errorf("unterminated multi-line string")
}

func (p *Parser) parseStringList() (*ListValue, error) {
	if p.pos >= p.length || p.input[p.pos] != '[' {
		return nil, fmt.Errorf("expected '[' at position %d", p.pos)
	}
	p.pos++ // skip '['

	list := &ListValue{Values: []string{}}

	for p.pos < p.length && p.input[p.pos] != ']' {
		p.skipWhitespaceAndComments()
		// skipWhitespaceAndComments can consume the rest of the input, so the
		// bound must be re-checked before indexing p.input again.
		if p.pos >= p.length {
			break
		}
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
	// F5341: an out-of-range number must fail the script, not become 0.
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid number %q", value)
	}

	// F5006: RFC 5228 §2.4.1 number = 1*DIGIT [QUANTIFIER], QUANTIFIER = "K" / "M" / "G".
	if p.pos < p.length {
		shift := 0
		switch p.input[p.pos] {
		case 'K', 'k':
			shift = 10
		case 'M', 'm':
			shift = 20
		case 'G', 'g':
			shift = 30
		}
		if shift != 0 && (p.pos+1 >= p.length || !isAlnumUnderscore(p.input[p.pos+1])) {
			p.pos++
			if n > math.MaxInt64>>shift || n < math.MinInt64>>shift {
				return nil, fmt.Errorf("number %s%c out of range", value, p.input[p.pos-1])
			}
			n <<= shift
		}
	}

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
			fmt.Fprintf(&sb, "%v", arg)
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
