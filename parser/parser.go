package parser

import (
	"compiler101/ast"
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// Parser is the recursive-descent front end: one function per grammar rule,
// precedence encoded in which function calls which. It is the algorithm from
// chapter 6 of the book, and one of the two implementations of Algorithm —
// parser/llk is the other. See docs/llk-parser.md for what the two trade.
type Parser struct {
	Tokens  []token.Token
	Current int
}

// ParseError is an alias, not a new type: the LL(k) front end returns the same
// error values, and `err.(*parser.ParseError)` has to keep working on them.
type ParseError = errors.ParseError

func errorAt(t token.Token, message string) error {
	return errors.ParseErrorAt(t, message)
}

func New(tokens []token.Token) *Parser {
	if len(tokens) == 0 || tokens[len(tokens)-1].Type != token.EOF {
		line := 1
		if len(tokens) > 0 {
			line = tokens[len(tokens)-1].Line
		}
		tokens = append(tokens[:len(tokens):len(tokens)], token.Token{Type: token.EOF, Line: line})
	}
	return &Parser{Tokens: tokens}
}

func (p *Parser) Parse() (ast.Expr, error) {
	expr, err := p.expression()
	if err != nil {
		return nil, err
	}

	if !p.isAtEnd() {
		return nil, errorAt(p.peek(), "Expect end of expression.")
	}
	return expr, nil
}

func (p *Parser) ParseAll() ([]ast.Expr, []error) {
	var exprs []ast.Expr
	var errs []error

	for !p.isAtEnd() {
		expr, err := p.expressionStatement()
		if err != nil {
			errs = append(errs, err)
			p.synchronize()
			continue
		}
		exprs = append(exprs, expr)
	}

	return exprs, errs
}

func (p *Parser) expressionStatement() (ast.Expr, error) {
	expr, err := p.expression()
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.SEMICOLON, "Expect ';' after expression."); err != nil {
		return nil, err
	}
	return expr, nil
}

func (p *Parser) synchronize() {
	p.advance()
	for !p.isAtEnd() {
		if p.previous().Type == token.SEMICOLON {
			return
		}
		switch p.peek().Type {
		case token.CLASS, token.FUN, token.VAR, token.FOR,
			token.IF, token.WHILE, token.PRINT, token.RETURN:
			return
		}
		p.advance()
	}
}

func (p *Parser) expression() (ast.Expr, error) {
	return p.equality()
}

func (p *Parser) equality() (ast.Expr, error) {
	expr, err := p.comparison()
	if err != nil {
		return nil, err
	}

	for p.match(token.BANG_EQUAL, token.EQUAL_EQUAL) {
		operator := p.previous()
		right, err := p.comparison()
		if err != nil {
			return nil, err
		}
		expr = &ast.Binary{
			Left:     expr,
			Operator: operator,
			Right:    right,
		}
	}
	return expr, nil
}

func (p *Parser) comparison() (ast.Expr, error) {
	expr, err := p.term()
	if err != nil {
		return nil, err
	}

	for p.match(token.GREATER, token.GREATER_EQUAL, token.LESS, token.LESS_EQUAL) {
		operator := p.previous()
		right, err := p.term()
		if err != nil {
			return nil, err
		}
		expr = &ast.Binary{
			Left:     expr,
			Operator: operator,
			Right:    right,
		}
	}
	return expr, nil
}

func (p *Parser) term() (ast.Expr, error) {
	expr, err := p.factor()
	if err != nil {
		return nil, err
	}

	for p.match(token.MINUS, token.PLUS) {
		operator := p.previous()
		right, err := p.factor()
		if err != nil {
			return nil, err
		}
		expr = &ast.Binary{
			Left:     expr,
			Operator: operator,
			Right:    right,
		}
	}
	return expr, nil
}

func (p *Parser) factor() (ast.Expr, error) {
	expr, err := p.unary()
	if err != nil {
		return nil, err
	}

	for p.match(token.SLASH, token.STAR) {
		operator := p.previous()
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		expr = &ast.Binary{
			Left:     expr,
			Operator: operator,
			Right:    right,
		}
	}
	return expr, nil
}

func (p *Parser) unary() (ast.Expr, error) {
	if p.match(token.BANG, token.MINUS) {
		operator := p.previous()
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &ast.Unary{
			Operator: operator,
			Right:    right,
		}, nil
	}
	return p.primary()
}

func (p *Parser) primary() (ast.Expr, error) {
	switch {
	case p.match(token.FALSE):
		return &ast.Literal{Value: false}, nil
	case p.match(token.TRUE):
		return &ast.Literal{Value: true}, nil
	case p.match(token.NIL):
		return &ast.Literal{Value: nil}, nil

	case p.match(token.NUMBER, token.STRING):
		return &ast.Literal{Value: p.previous().Literal}, nil

	case p.match(token.LEFT_PAREN):
		expr, err := p.expression()
		if err != nil {
			return nil, err
		}
		if _, err := p.consume(token.RIGHT_PAREN, "Expect ')' after expression."); err != nil {
			return nil, err
		}
		return &ast.Grouping{Expression: expr}, nil
	}

	return nil, errorAt(p.peek(), "Expect expression.")
}

func (p *Parser) consume(tokenType token.TokenType, message string) (token.Token, error) {
	if p.check(tokenType) {
		return p.advance(), nil
	}
	return token.Token{}, errorAt(p.peek(), message)
}

func (p *Parser) match(types ...token.TokenType) bool {
	for _, tp := range types {
		if p.check(tp) {
			p.advance()
			return true
		}
	}

	return false
}

func (p *Parser) check(tp token.TokenType) bool {
	if p.isAtEnd() {
		return false
	}
	return p.peek().Type == tp
}

func (p *Parser) advance() token.Token {
	if !p.isAtEnd() {
		p.Current++
	}
	return p.previous()
}

func (p *Parser) isAtEnd() bool {
	return p.peek().Type == token.EOF
}

func (p *Parser) peek() token.Token {
	if p.Current < 0 || p.Current >= len(p.Tokens) {
		return token.Token{Type: token.EOF}
	}
	return p.Tokens[p.Current]
}

func (p *Parser) previous() token.Token {
	if p.Current <= 0 {
		return p.peek()
	}
	return p.Tokens[p.Current-1]
}
