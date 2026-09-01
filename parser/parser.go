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

	programErrors []error
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

// ParseProgram parses a complete Lox program. A program is a sequence of
// declarations terminated by EOF; errors are collected so a bad declaration
// does not hide later independent errors.
func (p *Parser) ParseProgram() ([]ast.Stmt, []error) {
	p.programErrors = p.programErrors[:0]
	var statements []ast.Stmt

	for !p.isAtEnd() {
		if stmt := p.declarationRecovering(); stmt != nil {
			statements = append(statements, stmt)
		}
	}

	return statements, append([]error(nil), p.programErrors...)
}

func (p *Parser) declarationRecovering() ast.Stmt {
	stmt, err := p.declaration()
	if err == nil {
		return stmt
	}
	p.programErrors = append(p.programErrors, err)
	p.synchronize()
	return nil
}

func (p *Parser) declaration() (ast.Stmt, error) {
	if p.match(token.CLASS) {
		return p.classDeclaration()
	}
	if p.match(token.FUN) {
		// Assigned rather than returned directly: function returns a typed
		// *ast.Function, and returning that as an ast.Stmt on the error path
		// would produce a non-nil interface holding a nil pointer, which
		// declarationRecovering's nil check would wave through.
		function, err := p.function("function")
		if err != nil {
			return nil, err
		}
		return function, nil
	}
	if p.match(token.VAR) {
		return p.varDeclaration()
	}
	return p.statement()
}

// classDeclaration parses a class body as a list of methods. A method is
// exactly the `function` rule without the leading `fun`: the keyword is
// redundant inside a class body, so the grammar drops it.
//
// A leading `class` makes the method a class method — one reached through the
// class rather than through an instance. Reusing the keyword costs nothing:
// `class` cannot start a method name, so one token of lookahead settles it.
//
// `< IDENTIFIER` names the superclass. It is parsed straight into a Variable
// rather than through expression(): the grammar allows a name and nothing else
// there, so `class A < f() {}` should fail at the `(`, not evaluate a call.
func (p *Parser) classDeclaration() (ast.Stmt, error) {
	name, err := p.consume(token.IDENTIFIER, "Expect class name.")
	if err != nil {
		return nil, err
	}

	var superclass *ast.Variable
	if p.match(token.LESS) {
		superName, err := p.consume(token.IDENTIFIER, "Expect superclass name.")
		if err != nil {
			return nil, err
		}
		superclass = &ast.Variable{Name: superName}
	}

	if _, err := p.consume(token.LEFT_BRACE, "Expect '{' before class body."); err != nil {
		return nil, err
	}

	var methods, classMethods []*ast.Function
	for !p.check(token.RIGHT_BRACE) && !p.isAtEnd() {
		onTheClass := p.match(token.CLASS)
		method, err := p.functionBody("method", true)
		if err != nil {
			return nil, err
		}
		if onTheClass {
			classMethods = append(classMethods, method)
		} else {
			methods = append(methods, method)
		}
	}

	if _, err := p.consume(token.RIGHT_BRACE, "Expect '}' after class body."); err != nil {
		return nil, err
	}
	return &ast.Class{Name: name, Superclass: superclass, Methods: methods, ClassMethods: classMethods}, nil
}

func (p *Parser) function(kind string) (*ast.Function, error) {
	return p.functionBody(kind, false)
}

// functionBody parses the `function` rule. allowGetter makes the parameter list
// optional, which is legal only inside a class body: a `{` straight after the
// name means the member is a getter, and its body runs on property access.
//
// A plain `fun` declaration never takes that path, so `fun f {}` stays the
// error it has always been.
func (p *Parser) functionBody(kind string, allowGetter bool) (*ast.Function, error) {
	name, err := p.consume(token.IDENTIFIER, "Expect "+kind+" name.")
	if err != nil {
		return nil, err
	}

	if allowGetter && p.check(token.LEFT_BRACE) {
		body, err := p.blockBody("Expect '{' before " + kind + " body.")
		if err != nil {
			return nil, err
		}
		return &ast.Function{Name: name, Body: body, IsGetter: true}, nil
	}

	if _, err := p.consume(token.LEFT_PAREN, "Expect '(' after "+kind+" name."); err != nil {
		return nil, err
	}

	var params []token.Token
	if !p.check(token.RIGHT_PAREN) {
		for {
			if len(params) >= 255 {
				errors.ErrorToken(p.peek(), "Can't have more than 255 parameters.")
			}
			param, err := p.consume(token.IDENTIFIER, "Expect parameter name.")
			if err != nil {
				return nil, err
			}
			params = append(params, param)
			if !p.match(token.COMMA) {
				break
			}
		}
	}

	if _, err := p.consume(token.RIGHT_PAREN, "Expect ')' after parameters."); err != nil {
		return nil, err
	}
	body, err := p.blockBody("Expect '{' before " + kind + " body.")
	if err != nil {
		return nil, err
	}
	return &ast.Function{Name: name, Params: params, Body: body}, nil
}

// blockBody consumes a brace-delimited body, reporting message when the opening
// brace is missing. It exists so the getter path and the ordinary path share
// one spelling of "a body is a block".
func (p *Parser) blockBody(message string) ([]ast.Stmt, error) {
	if _, err := p.consume(token.LEFT_BRACE, message); err != nil {
		return nil, err
	}
	return p.block()
}

func (p *Parser) varDeclaration() (ast.Stmt, error) {
	name, err := p.consume(token.IDENTIFIER, "Expect variable name.")
	if err != nil {
		return nil, err
	}

	var initializer ast.Expr
	if p.match(token.EQUAL) {
		initializer, err = p.expression()
		if err != nil {
			return nil, err
		}
	}

	if _, err := p.consume(token.SEMICOLON, "Expect ';' after variable declaration."); err != nil {
		return nil, err
	}
	return &ast.Var{Name: name, Initializer: initializer}, nil
}

func (p *Parser) statement() (ast.Stmt, error) {
	if p.match(token.FOR) {
		return p.forStatement()
	}
	if p.match(token.IF) {
		return p.ifStatement()
	}
	if p.match(token.PRINT) {
		return p.printStatement()
	}
	if p.match(token.RETURN) {
		return p.returnStatement()
	}
	if p.match(token.WHILE) {
		return p.whileStatement()
	}
	if p.match(token.LEFT_BRACE) {
		statements, err := p.block()
		if err != nil {
			return nil, err
		}
		return &ast.Block{Statements: statements}, nil
	}
	return p.expressionStmt()
}

func (p *Parser) forStatement() (ast.Stmt, error) {
	if _, err := p.consume(token.LEFT_PAREN, "Expect '(' after 'for'."); err != nil {
		return nil, err
	}

	var initializer ast.Stmt
	var err error
	switch {
	case p.match(token.SEMICOLON):
		// No initializer.
	case p.match(token.VAR):
		initializer, err = p.varDeclaration()
	default:
		initializer, err = p.expressionStmt()
	}
	if err != nil {
		return nil, err
	}

	var condition ast.Expr
	if !p.check(token.SEMICOLON) {
		condition, err = p.expression()
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.consume(token.SEMICOLON, "Expect ';' after loop condition."); err != nil {
		return nil, err
	}

	var increment ast.Expr
	if !p.check(token.RIGHT_PAREN) {
		increment, err = p.expression()
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.consume(token.RIGHT_PAREN, "Expect ')' after for clauses."); err != nil {
		return nil, err
	}

	body, err := p.statement()
	if err != nil {
		return nil, err
	}
	if increment != nil {
		body = &ast.Block{Statements: []ast.Stmt{
			body,
			&ast.Expression{Expression: increment},
		}}
	}
	if condition == nil {
		condition = &ast.Literal{Value: true}
	}
	body = &ast.While{Condition: condition, Body: body}
	if initializer != nil {
		body = &ast.Block{Statements: []ast.Stmt{initializer, body}}
	}

	return body, nil
}

func (p *Parser) ifStatement() (ast.Stmt, error) {
	if _, err := p.consume(token.LEFT_PAREN, "Expect '(' after 'if'."); err != nil {
		return nil, err
	}
	condition, err := p.expression()
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.RIGHT_PAREN, "Expect ')' after if condition."); err != nil {
		return nil, err
	}

	thenBranch, err := p.statement()
	if err != nil {
		return nil, err
	}
	var elseBranch ast.Stmt
	if p.match(token.ELSE) {
		elseBranch, err = p.statement()
		if err != nil {
			return nil, err
		}
	}

	return &ast.If{Condition: condition, ThenBranch: thenBranch, ElseBranch: elseBranch}, nil
}

func (p *Parser) whileStatement() (ast.Stmt, error) {
	if _, err := p.consume(token.LEFT_PAREN, "Expect '(' after 'while'."); err != nil {
		return nil, err
	}
	condition, err := p.expression()
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.RIGHT_PAREN, "Expect ')' after condition."); err != nil {
		return nil, err
	}
	body, err := p.statement()
	if err != nil {
		return nil, err
	}

	return &ast.While{Condition: condition, Body: body}, nil
}

func (p *Parser) printStatement() (ast.Stmt, error) {
	value, err := p.expression()
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.SEMICOLON, "Expect ';' after value."); err != nil {
		return nil, err
	}
	return &ast.Print{Expression: value}, nil
}

func (p *Parser) returnStatement() (ast.Stmt, error) {
	keyword := p.previous()
	var value ast.Expr
	var err error
	if !p.check(token.SEMICOLON) {
		value, err = p.expression()
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.consume(token.SEMICOLON, "Expect ';' after return value."); err != nil {
		return nil, err
	}
	return &ast.Return{Keyword: keyword, Value: value}, nil
}

func (p *Parser) expressionStmt() (ast.Stmt, error) {
	expr, err := p.expression()
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.SEMICOLON, "Expect ';' after expression."); err != nil {
		return nil, err
	}
	return &ast.Expression{Expression: expr}, nil
}

func (p *Parser) block() ([]ast.Stmt, error) {
	var statements []ast.Stmt
	for !p.check(token.RIGHT_BRACE) && !p.isAtEnd() {
		if stmt := p.declarationRecovering(); stmt != nil {
			statements = append(statements, stmt)
		}
	}

	if _, err := p.consume(token.RIGHT_BRACE, "Expect '}' after block."); err != nil {
		return nil, err
	}
	return statements, nil
}

func (p *Parser) ParseAll() ([]ast.Expr, []error) {
	var exprs []ast.Expr
	var errs []error

	for !p.isAtEnd() {
		expr, err := p.expressionStatementExpr()
		if err != nil {
			errs = append(errs, err)
			p.synchronize()
			continue
		}
		exprs = append(exprs, expr)
	}

	return exprs, errs
}

// expressionStatementExpr is the chapter 7 batch parser retained for tests
// and callers that specifically want a list of expression trees.
func (p *Parser) expressionStatementExpr() (ast.Expr, error) {
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
	return p.assignment()
}

func (p *Parser) assignment() (ast.Expr, error) {
	expr, err := p.or()
	if err != nil {
		return nil, err
	}

	if p.match(token.EQUAL) {
		equals := p.previous()
		value, err := p.assignment()
		if err != nil {
			return nil, err
		}

		// The left side was already parsed as an ordinary expression; which
		// assignment node it becomes is decided by what it turned out to be.
		// That one-token-of-hindsight trick is why property assignment needs no
		// backtracking.
		switch target := expr.(type) {
		case *ast.Variable:
			return &ast.Assign{Name: target.Name, Value: value}, nil
		case *ast.Get:
			return &ast.Set{Object: target.Object, Name: target.Name, Value: value}, nil
		}
		return nil, errorAt(equals, "Invalid assignment target.")
	}

	return expr, nil
}

func (p *Parser) or() (ast.Expr, error) {
	expr, err := p.and()
	if err != nil {
		return nil, err
	}

	for p.match(token.OR) {
		operator := p.previous()
		right, err := p.and()
		if err != nil {
			return nil, err
		}
		expr = &ast.Logical{Left: expr, Operator: operator, Right: right}
	}
	return expr, nil
}

func (p *Parser) and() (ast.Expr, error) {
	expr, err := p.equality()
	if err != nil {
		return nil, err
	}

	for p.match(token.AND) {
		operator := p.previous()
		right, err := p.equality()
		if err != nil {
			return nil, err
		}
		expr = &ast.Logical{Left: expr, Operator: operator, Right: right}
	}
	return expr, nil
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
	return p.call()
}

func (p *Parser) call() (ast.Expr, error) {
	expr, err := p.primary()
	if err != nil {
		return nil, err
	}

	// Calls and property accesses are both postfix and bind equally tightly, so
	// they interleave freely: egg.scramble(3).with(cheese).
	for {
		switch {
		case p.match(token.LEFT_PAREN):
			expr, err = p.finishCall(expr)
			if err != nil {
				return nil, err
			}
		case p.match(token.DOT):
			name, err := p.consume(token.IDENTIFIER, "Expect property name after '.'.")
			if err != nil {
				return nil, err
			}
			expr = &ast.Get{Object: expr, Name: name}
		default:
			return expr, nil
		}
	}
}

func (p *Parser) finishCall(callee ast.Expr) (ast.Expr, error) {
	var arguments []ast.Expr
	if !p.check(token.RIGHT_PAREN) {
		for {
			if len(arguments) >= 255 {
				errors.ErrorToken(p.peek(), "Can't have more than 255 arguments.")
			}
			argument, err := p.expression()
			if err != nil {
				return nil, err
			}
			arguments = append(arguments, argument)
			if !p.match(token.COMMA) {
				break
			}
		}
	}

	paren, err := p.consume(token.RIGHT_PAREN, "Expect ')' after arguments.")
	if err != nil {
		return nil, err
	}
	return &ast.Call{Callee: callee, Paren: paren, Arguments: arguments}, nil
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
	case p.match(token.SUPER):
		keyword := p.previous()
		// `super` is never an expression on its own: it names no value, only a
		// place to start a method lookup. Requiring the `.` here is what makes
		// that a parse error rather than something the resolver has to catch.
		if _, err := p.consume(token.DOT, "Expect '.' after 'super'."); err != nil {
			return nil, err
		}
		method, err := p.consume(token.IDENTIFIER, "Expect superclass method name.")
		if err != nil {
			return nil, err
		}
		return &ast.Super{Keyword: keyword, Method: method}, nil
	case p.match(token.THIS):
		return &ast.This{Keyword: p.previous()}, nil
	case p.match(token.IDENTIFIER):
		return &ast.Variable{Name: p.previous()}, nil

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
