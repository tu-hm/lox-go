package scanner

import (
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
	"compiler101/utils"
	"strconv"
)

var keywords = map[string]token.TokenType{
	"and":    token.AND,
	"class":  token.CLASS,
	"else":   token.ELSE,
	"false":  token.FALSE,
	"for":    token.FOR,
	"fun":    token.FUN,
	"if":     token.IF,
	"nil":    token.NIL,
	"or":     token.OR,
	"print":  token.PRINT,
	"return": token.RETURN,
	"super":  token.SUPER,
	"this":   token.THIS,
	"true":   token.TRUE,
	"var":    token.VAR,
	"while":  token.WHILE,
}

type Scanner interface {
	ScanToken() []token.Token
}

type ScannerImpl struct {
	Source    string
	ListToken []token.Token

	start   int
	current int
	line    int
}

var _ Scanner = &ScannerImpl{}

func NewScanner(source string) Scanner {
	return &ScannerImpl{
		Source: source,
		line:   1,
	}
}

func (s *ScannerImpl) ScanToken() []token.Token {
	for !s.isAtEnd() {
		s.start = s.current
		s.scanToken()
	}

	s.ListToken = append(s.ListToken, token.Token{
		Type:    token.EOF,
		Lexeme:  "",
		Literal: nil,
		Line:    s.line,
	})
	return s.ListToken
}

func (s *ScannerImpl) isAtEnd() bool {
	return s.current >= len(s.Source)
}

func (s *ScannerImpl) advance() byte {
	c := s.Source[s.current]
	s.current++
	return c
}

// addToken records a token with no literal value. Only NUMBER and STRING
// carry literals; everything else must leave it nil so the interpreter can
// tell "no value" from a value.
func (s *ScannerImpl) addToken(t token.TokenType) {
	s.addTokenWithValue(t, nil)
}

func (s *ScannerImpl) addTokenWithValue(t token.TokenType, literal any) {
	text := s.Source[s.start:s.current]
	s.ListToken = append(s.ListToken, token.Token{
		Type:    t,
		Lexeme:  text,
		Literal: literal,
		Line:    s.line,
	})
}

func (s *ScannerImpl) match(expected byte) bool {
	if s.isAtEnd() {
		return false
	}

	if s.Source[s.current] != expected {
		return false
	}

	s.current++
	return true
}

func (s *ScannerImpl) peek() byte {
	if s.isAtEnd() {
		return '\x00'
	}
	return s.Source[s.current]
}

func (s *ScannerImpl) scanToken() {
	c := s.advance()

	switch c {
	case '(':
		s.addToken(token.LEFT_PAREN)
	case ')':
		s.addToken(token.RIGHT_PAREN)
	case '{':
		s.addToken(token.LEFT_BRACE)
	case '}':
		s.addToken(token.RIGHT_BRACE)
	case ',':
		s.addToken(token.COMMA)
	case '.':
		s.addToken(token.DOT)
	case '-':
		s.addToken(token.MINUS)
	case '+':
		s.addToken(token.PLUS)
	case ';':
		s.addToken(token.SEMICOLON)
	case '*':
		s.addToken(token.STAR)

	case '!':
		if s.match('=') {
			s.addToken(token.BANG_EQUAL)
		} else {
			s.addToken(token.BANG)
		}

	case '=':
		if s.match('=') {
			s.addToken(token.EQUAL_EQUAL)
		} else {
			s.addToken(token.EQUAL)
		}

	case '<':
		if s.match('=') {
			s.addToken(token.LESS_EQUAL)
		} else {
			s.addToken(token.LESS)
		}

	case '>':
		if s.match('=') {
			s.addToken(token.GREATER_EQUAL)
		} else {
			s.addToken(token.GREATER)
		}

	case '/':
		if s.match('/') {
			for s.peek() != '\n' && !s.isAtEnd() {
				s.advance()
			}
		} else {
			s.addToken(token.SLASH)
		}

	case '"':
		s.stringLiteral()

	case ' ', '\r', '\t':

	case '\n':
		s.line++

	default:
		if utils.IsDigit(c) {
			s.number()
		} else if utils.IsAlpha(c) {
			s.identifier()
		} else {
			errors.Error(s.line, "Unexpected character.")
		}
	}
}

// stringLiteral scans a double-quoted string. Lox strings may span newlines,
// so the line counter is advanced as we go. Named stringLiteral rather than
// the book's string() to avoid reading like the builtin type.
func (s *ScannerImpl) stringLiteral() {
	for s.peek() != '"' && !s.isAtEnd() {
		if s.peek() == '\n' {
			s.line++
		}
		s.advance()
	}

	if s.isAtEnd() {
		errors.Error(s.line, "Unterminated string.")
		return
	}
	s.advance()

	value := s.Source[s.start+1 : s.current-1]
	s.addTokenWithValue(token.STRING, value)
}

func (s *ScannerImpl) number() {
	for utils.IsDigit(s.peek()) {
		s.advance()
	}

	if s.peek() == '.' && utils.IsDigit(s.peekNext()) {
		s.advance()

		for utils.IsDigit(s.peek()) {
			s.advance()
		}
	}

	value, err := strconv.ParseFloat(s.Source[s.start:s.current], 64)
	if err != nil {
		errors.Error(s.line, "Number literal is too large.")
		return
	}

	s.addTokenWithValue(token.NUMBER, value)
}

func (s *ScannerImpl) identifier() {
	for utils.IsAlphaNumeric(s.peek()) {
		s.advance()
	}

	text := s.Source[s.start:s.current]
	t, ok := keywords[text]
	if !ok {
		t = token.IDENTIFIER
	}

	s.addToken(t)
}

func (s *ScannerImpl) peekNext() byte {
	if s.current+1 >= len(s.Source) {
		return '\x00'
	}
	return s.Source[s.current+1]
}
