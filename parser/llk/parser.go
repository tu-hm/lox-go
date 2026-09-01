// Package llk is the second of this repo's two parser front ends: a
// table-driven LL(k) parser for expressions and leaf statements, with a small
// recursive orchestration layer for recoverable blocks and compound control
// statements. It produces the same trees as parser's recursive-descent front
// end.
//
// The difference is where the decisions live. Recursive descent spreads them
// across a function per rule, made at run time by looking at the next token.
// This parser makes them ahead of time: it computes FIRST_k and FOLLOW_k for
// the grammar (sets.go), turns them into a prediction table (table.go), and
// then parses with a loop over an explicit stack — no recursion, no per-rule
// code, and one k-string lookup per expansion.
//
// What you get for that is a grammar that can be *checked*: if two productions
// of the same rule are ambiguous at k tokens of lookahead, the table refuses to
// build and names both. A hand-written parser cannot tell you that; it just
// picks the branch written first.
//
// What you give up is freedom. Left recursion has to be rewritten away, the
// error messages come from a table rather than from context, and k is a
// constant chosen up front. docs/llk-parser.md walks through all of it.
package llk

import (
	"fmt"

	"compiler101/ast"
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// Bounds on the lookahead. k = 1 already parses this grammar — the current Lox
// grammar is LL(1) — so larger k is here to show that the machinery
// is general, not because the language needs it.
//
// The ceiling is measured, not arbitrary. FIRST_k and FOLLOW_k hold strings of
// k terminals, so the table grows steeply with k. That curve is the practical
// reason real tools stop at LL(1) plus a hack, or move to LL(*)/PEG, rather
// than raising k.
const (
	MinK     = 1
	MaxK     = 3
	DefaultK = 1
)

// Parser holds the position in the token stream plus the two stacks the driver
// runs on: work, the grammar symbols still to match, and vals, the AST nodes
// built so far. The recursive-descent parser keeps both of those in the Go call
// stack instead, which is exactly what makes it shorter and this one inspectable.
type Parser struct {
	Tokens  []token.Token
	Current int

	tab  *table
	work []item // parse stack, top at the end
	vals stack  // value stack
	la   seq    // reusable lookahead window, so predicting allocates nothing

	// lookaheadLimit is one past a delimiter that a nested expression run may
	// inspect. Tokens after it are masked as EOF so k > 1 cannot leak from an
	// if/while/for header into the following statement.
	lookaheadLimit int

	programErrors []error
}

// New builds a parser with the default lookahead. It panics if the table cannot
// be built, in the spirit of regexp.MustCompile: the grammar is a compile-time
// constant of this package, so a failure here is a bug in the grammar, not in
// anything a caller did. Use NewK when k comes from outside the program.
func New(tokens []token.Token) *Parser {
	p, err := NewK(tokens, DefaultK)
	if err != nil {
		panic(err)
	}
	return p
}

// NewK builds a parser with k tokens of lookahead. The error is a
// *ConflictError when the grammar is not LL(k), and a plain error when k is out
// of range.
func NewK(tokens []token.Token, k int) (*Parser, error) {
	tab, err := tableFor(k)
	if err != nil {
		return nil, err
	}
	return &Parser{Tokens: withEOF(tokens), tab: tab}, nil
}

// withEOF guarantees the sentinel the whole package assumes: lookahead windows
// are padded with EOF, and FOLLOW sets are seeded with it, so a stream that
// lacks one would run off the end instead of stopping.
func withEOF(tokens []token.Token) []token.Token {
	if len(tokens) > 0 && tokens[len(tokens)-1].Type == token.EOF {
		return tokens
	}
	line := 1
	if len(tokens) > 0 {
		line = tokens[len(tokens)-1].Line
	}
	return append(tokens[:len(tokens):len(tokens)], token.Token{Type: token.EOF, Line: line})
}

// K reports the lookahead this parser was built with.
func (p *Parser) K() int { return p.tab.k }

// Parse reads one expression and requires the stream to end there.
func (p *Parser) Parse() (ast.Expr, error) {
	expr, err := p.expression()
	if err != nil {
		return nil, err
	}
	if !p.isAtEnd() {
		return nil, errors.ParseErrorAt(p.peek(), expectEnd)
	}
	return expr, nil
}

func (p *Parser) expression() (ast.Expr, error) {
	value, err := p.run(nExpression)
	if err != nil {
		return nil, err
	}
	expr, ok := value.(ast.Expr)
	if !ok {
		return nil, fmt.Errorf("llk: internal error: %s produced %T, want ast.Expr", nExpression, value)
	}
	return expr, nil
}

// ParseProgram parses declarations until EOF. Statement selection and block
// recovery are kept at this small orchestration layer; expression parsing and
// each non-block statement shape still run through the checked LL(k) table.
// Starting the driver afresh at each declaration preserves the parser's
// existing recovery invariant: no half-expanded stack survives an error.
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
	if p.check(token.CLASS) {
		p.advance()
		return p.classDeclaration()
	}
	if p.check(token.FUN) {
		p.advance()
		// Assigned rather than returned directly: function returns a typed
		// *ast.Function, so returning it as an ast.Stmt on the error path would
		// hand back a non-nil interface holding a nil pointer.
		function, err := p.function("function")
		if err != nil {
			return nil, err
		}
		return function, nil
	}
	if p.check(token.VAR) {
		return p.runStatement(nVarDeclaration)
	}
	return p.statement()
}

// classDeclaration joins the orchestration layer for the same reason function
// does: a class body is a repetition of a rule that itself contains a
// recoverable block, and recovery is what this layer exists for.
//
// The superclass clause is read here rather than through the table for the
// same reason: it is one optional token pair in the middle of that repetition.
// `super` itself is a table production, because it is an expression.
func (p *Parser) classDeclaration() (ast.Stmt, error) {
	name, err := p.consume(token.IDENTIFIER, "Expect class name.")
	if err != nil {
		return nil, err
	}

	var superclass *ast.Variable
	if p.check(token.LESS) {
		p.advance()
		superName, err := p.consume(token.IDENTIFIER, "Expect superclass name.")
		if err != nil {
			return nil, err
		}
		superclass = &ast.Variable{Name: superName}
	}

	if !p.check(token.LEFT_BRACE) {
		return nil, errors.ParseErrorAt(p.peek(), "Expect '{' before class body.")
	}
	p.advance()

	// A leading `class` marks a class method. One token of lookahead settles
	// it, because `class` cannot begin a method name.
	var methods, classMethods []*ast.Function
	for !p.check(token.RIGHT_BRACE) && !p.isAtEnd() {
		onTheClass := p.check(token.CLASS)
		if onTheClass {
			p.advance()
		}
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
func (p *Parser) functionBody(kind string, allowGetter bool) (*ast.Function, error) {
	name, err := p.consume(token.IDENTIFIER, "Expect "+kind+" name.")
	if err != nil {
		return nil, err
	}

	if allowGetter && p.check(token.LEFT_BRACE) {
		body, err := p.blockStatements()
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
			if !p.check(token.COMMA) {
				break
			}
			p.advance()
		}
	}

	if _, err := p.consume(token.RIGHT_PAREN, "Expect ')' after parameters."); err != nil {
		return nil, err
	}
	if !p.check(token.LEFT_BRACE) {
		return nil, errors.ParseErrorAt(p.peek(), "Expect '{' before "+kind+" body.")
	}
	body, err := p.blockStatements()
	if err != nil {
		return nil, err
	}
	return &ast.Function{Name: name, Params: params, Body: body}, nil
}

// blockStatements parses a body and unwraps it. A body is a block as far as the
// orchestration layer is concerned, but ast.Function stores the statements
// directly, so the assertion happens once here rather than at each caller.
func (p *Parser) blockStatements() ([]ast.Stmt, error) {
	body, err := p.block()
	if err != nil {
		return nil, err
	}
	block, ok := body.(*ast.Block)
	if !ok {
		return nil, fmt.Errorf("llk: internal error: function body produced %T, want *ast.Block", body)
	}
	return block.Statements, nil
}

func (p *Parser) statement() (ast.Stmt, error) {
	switch {
	case p.check(token.FOR):
		p.advance()
		return p.forStatement()
	case p.check(token.IF):
		p.advance()
		return p.ifStatement()
	case p.check(token.PRINT):
		return p.runStatement(nPrintStatement)
	case p.check(token.RETURN):
		return p.runStatement(nReturnStatement)
	case p.check(token.WHILE):
		p.advance()
		return p.whileStatement()
	case p.check(token.LEFT_BRACE):
		return p.block()
	default:
		return p.runStatement(nExprStatement)
	}
}

func (p *Parser) forStatement() (ast.Stmt, error) {
	if _, err := p.consume(token.LEFT_PAREN, "Expect '(' after 'for'."); err != nil {
		return nil, err
	}

	var initializer ast.Stmt
	var err error
	switch {
	case p.check(token.SEMICOLON):
		p.advance()
	case p.check(token.VAR):
		initializer, err = p.runStatement(nVarDeclaration)
	default:
		initializer, err = p.runStatement(nExprStatement)
	}
	if err != nil {
		return nil, err
	}

	var condition ast.Expr
	if !p.check(token.SEMICOLON) {
		condition, err = p.expressionBefore(token.SEMICOLON)
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.consume(token.SEMICOLON, "Expect ';' after loop condition."); err != nil {
		return nil, err
	}

	var increment ast.Expr
	if !p.check(token.RIGHT_PAREN) {
		increment, err = p.expressionBefore(token.RIGHT_PAREN)
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
	condition, err := p.expressionBefore(token.RIGHT_PAREN)
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
	if p.check(token.ELSE) {
		p.advance()
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
	condition, err := p.expressionBefore(token.RIGHT_PAREN)
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

func (p *Parser) runStatement(start string) (ast.Stmt, error) {
	previous := p.lookaheadLimit
	if limit := p.delimiterLimit(token.SEMICOLON); limit != 0 {
		p.lookaheadLimit = limit
	}
	defer func() { p.lookaheadLimit = previous }()

	value, err := p.run(start)
	if err != nil {
		return nil, err
	}
	stmt, ok := value.(ast.Stmt)
	if !ok {
		return nil, fmt.Errorf("llk: internal error: %s produced %T, want ast.Stmt", start, value)
	}
	return stmt, nil
}

func (p *Parser) expressionBefore(delimiter token.TokenType) (ast.Expr, error) {
	limit := p.delimiterLimit(delimiter)
	previous := p.lookaheadLimit
	p.lookaheadLimit = limit
	defer func() { p.lookaheadLimit = previous }()

	return p.expression()
}

// delimiterLimit finds the first delimiter outside nested parentheses and
// returns the exclusive lookahead boundary just after it. A missing delimiter
// leaves lookahead unbounded so the expression parser can report its own error
// before the caller reports the missing punctuation.
func (p *Parser) delimiterLimit(delimiter token.TokenType) int {
	depth := 0
	for at := p.Current; at < len(p.Tokens); at++ {
		switch p.Tokens[at].Type {
		case token.LEFT_PAREN:
			depth++
		case token.RIGHT_PAREN:
			if depth == 0 && delimiter == token.RIGHT_PAREN {
				return at + 1
			}
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && p.Tokens[at].Type == delimiter {
				return at + 1
			}
		}
	}
	return 0
}

func (p *Parser) block() (ast.Stmt, error) {
	p.advance() // '{'
	var statements []ast.Stmt
	for !p.check(token.RIGHT_BRACE) && !p.isAtEnd() {
		if stmt := p.declarationRecovering(); stmt != nil {
			statements = append(statements, stmt)
		}
	}
	if _, err := p.consume(token.RIGHT_BRACE, "Expect '}' after block."); err != nil {
		return nil, err
	}
	return &ast.Block{Statements: statements}, nil
}

// ParseAll reads a batch of ';'-terminated expressions, recovering after each
// error so that one bad statement costs one message instead of a cascade.
//
// Each statement is a fresh run of the driver rather than one run over the
// whole program, because recovery is easier to reason about with an empty
// stack: after synchronize, there is nothing half-expanded left to unwind.
func (p *Parser) ParseAll() ([]ast.Expr, []error) {
	var exprs []ast.Expr
	var errs []error

	for !p.isAtEnd() {
		value, err := p.run(nExprStatement)
		if err != nil {
			errs = append(errs, err)
			p.synchronize()
			continue
		}
		stmt, ok := value.(*ast.Expression)
		if !ok {
			errs = append(errs, fmt.Errorf("llk: internal error: %s produced %T", nExprStatement, value))
			continue
		}
		exprs = append(exprs, stmt.Expression)
	}

	return exprs, errs
}

// run is the whole parsing algorithm.
//
// Push the start symbol; then repeatedly take the top of the stack and do the
// one thing its kind calls for — match a terminal against the input, run an
// action, or replace a nonterminal by the production the table predicts. The
// body goes on reversed so its leftmost symbol comes off first.
//
// Everything about prediction has already happened in table.go. That is the
// point: this loop is the same for any LL(k) grammar.
func (p *Parser) run(start string) (any, error) {
	p.work = append(p.work[:0], nt(start))
	p.vals.reset()

	for len(p.work) > 0 {
		it := p.work[len(p.work)-1]
		p.work = p.work[:len(p.work)-1]

		switch it.kind {
		case itemAction:
			if err := it.act(&p.vals); err != nil {
				return nil, err
			}

		case itemTerminal:
			if !p.check(it.term) {
				return nil, errors.ParseErrorAt(p.peek(), p.terminalMessage(it))
			}
			p.vals.push(p.advance())

		case itemNonterminal:
			prod, ok := p.tab.predict(it.name, p.window())
			if !ok {
				// Prediction missed, so the input is already known to be bad.
				// Expanding the closest production anyway lets the failure
				// surface at the terminal that actually disagrees, carrying
				// that terminal's own message instead of the rule's generic
				// one. See table.recover for why this cannot accept a bad
				// program or fail to terminate.
				prod, ok = p.tab.recover(it.name, p.window())
				if !ok {
					return nil, errors.ParseErrorAt(p.peek(), p.tab.fail(it.name))
				}
			}
			for i := len(prod.body) - 1; i >= 0; i-- {
				p.work = append(p.work, prod.body[i])
			}
		}
	}

	// One node, and nothing else: every token pushed has been folded into the
	// tree or explicitly discarded. A failure here means an action pops the
	// wrong number of values, which is a bug in grammar.go rather than in the
	// input, so it is not reported as a syntax error.
	if len(p.vals.vals) != 1 {
		return nil, fmt.Errorf("llk: internal error: %d values left on the stack after %s", len(p.vals.vals), start)
	}
	return p.vals.pop(), nil
}

// terminalMessage prefers the message the grammar attached to this position
// ("Expect ')' after expression.") and falls back to naming the terminal.
func (p *Parser) terminalMessage(it item) string {
	if it.msg != "" {
		return it.msg
	}
	return "Expect " + describe(it.term) + "."
}

// window is the next k tokens as a k-string, cut at EOF: the table's column
// index. The buffer is reused because predict only ever reads it.
func (p *Parser) window() seq {
	w := p.la[:0]
	for i := 0; i < p.tab.k; i++ {
		t := token.EOF
		if (p.lookaheadLimit == 0 || p.Current+i < p.lookaheadLimit) && p.Current+i < len(p.Tokens) {
			t = p.Tokens[p.Current+i].Type
		}
		w = append(w, t)
		if t == token.EOF {
			break
		}
	}
	p.la = w
	return w
}

// synchronize is the same recovery rule as the recursive-descent parser's, and
// deliberately so: discard tokens until just past a ';' or just before a token
// that can only start a statement, so the next parse begins somewhere
// plausible. It always consumes at least one token, which is what keeps
// ParseAll's loop from spinning.
//
// The stacks are dropped rather than repaired. Whatever was half-expanded when
// the error hit describes a parse that is not going to happen.
func (p *Parser) synchronize() {
	p.work = p.work[:0]
	p.vals.reset()

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

func (p *Parser) check(tp token.TokenType) bool { return p.peek().Type == tp }

func (p *Parser) consume(tp token.TokenType, message string) (token.Token, error) {
	if p.check(tp) {
		return p.advance(), nil
	}
	return token.Token{}, errors.ParseErrorAt(p.peek(), message)
}

func (p *Parser) advance() token.Token {
	if !p.isAtEnd() {
		p.Current++
	}
	return p.previous()
}

func (p *Parser) isAtEnd() bool { return p.peek().Type == token.EOF }

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
