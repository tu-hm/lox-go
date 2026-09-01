package llk

import (
	"strings"

	"compiler101/ast"
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// A grammar here is data, not code. The recursive-descent parser encodes the
// same rules as Go functions; this one encodes them as productions a table
// generator can read, which is the whole difference between the two front ends.
//
// Three things a plain BNF production does not have, and this one does:
//
//   - Semantic actions sit *inside* the body, at the position where they run.
//     That is what keeps `1 - 2 - 3` left-associative — see level().
//   - Terminals carry the error message to print when they fail to match, so
//     "Expect ')' after expression." lives next to the ')' that expects it.
//   - Nonterminals carry a message for when no production can be predicted.

type itemKind int

const (
	itemTerminal itemKind = iota
	itemNonterminal
	itemAction
)

// item is one element of a production body.
type item struct {
	kind itemKind
	term token.TokenType // itemTerminal
	msg  string          // itemTerminal: message when the match fails
	name string          // itemNonterminal
	act  action          // itemAction
}

func term(t token.TokenType, msg string) item {
	return item{kind: itemTerminal, term: t, msg: msg}
}

func nt(name string) item { return item{kind: itemNonterminal, name: name} }

func act(a action) item { return item{kind: itemAction, act: a} }

func (i item) String() string {
	switch i.kind {
	case itemTerminal:
		return string(i.term)
	case itemNonterminal:
		return i.name
	default:
		return "{action}"
	}
}

type production struct {
	head string
	body []item
}

// String renders a production the way docs/grammar.md writes it. Actions are
// left out: they are implementation, not language.
func (p production) String() string {
	parts := make([]string, 0, len(p.body))
	for _, i := range p.body {
		if i.kind == itemAction {
			continue
		}
		parts = append(parts, i.String())
	}
	if len(parts) == 0 {
		return p.head + " → ε"
	}
	return p.head + " → " + strings.Join(parts, " ")
}

type grammar struct {
	prods   []production
	order   []string          // nonterminals, in declaration order
	byHead  map[string][]int  // head → indexes into prods
	fail    map[string]string // head → message when nothing can be predicted
	entries []string          // nonterminals the driver may start from
}

func newGrammar() *grammar {
	return &grammar{byHead: map[string][]int{}, fail: map[string]string{}}
}

func (g *grammar) add(head string, body ...item) {
	if _, seen := g.byHead[head]; !seen {
		g.order = append(g.order, head)
	}
	g.byHead[head] = append(g.byHead[head], len(g.prods))
	g.prods = append(g.prods, production{head: head, body: body})
}

// entry marks a nonterminal the parser can be started at. Every entry gets EOF
// seeded into its FOLLOW set: starting there means the input is allowed to end
// there, and without the seed no ε-production could ever be predicted at the
// end of the stream.
func (g *grammar) entry(name, message string) {
	g.entries = append(g.entries, name)
	g.fail[name] = message
}

// validate catches the two ways a hand-written grammar goes wrong: a body that
// names a nonterminal nobody defines, and an entry point that does not exist.
// Both would otherwise show up as an empty FIRST set and a parse that fails on
// every input, which is a miserable thing to debug.
func (g *grammar) validate() error {
	for _, p := range g.prods {
		for _, i := range p.body {
			if i.kind == itemNonterminal && len(g.byHead[i.name]) == 0 {
				return &GrammarError{Message: p.String() + ": no production for " + i.name}
			}
		}
	}
	for _, e := range g.entries {
		if len(g.byHead[e]) == 0 {
			return &GrammarError{Message: "entry point " + e + " has no production"}
		}
	}
	return nil
}

// GrammarError is a bug in the grammar above, not in the user's source.
type GrammarError struct{ Message string }

func (e *GrammarError) Error() string { return "llk: " + e.Message }

// ------------------------------------------------------------- the grammar --

const (
	nProgram         = "program"
	nDeclarations    = "declarations"
	nDeclaration     = "declaration"
	nStatement       = "statement"
	nVarDeclaration  = "varDeclaration"
	nVarInitializer  = "varInitializer"
	nPrintStatement  = "printStatement"
	nReturnStatement = "returnStatement"
	nReturnValue     = "returnValue"
	nExprStatement   = "exprStatement"
	nBlock           = "block"
	nExpression      = "expression"
	nAssignment      = "assignment"
	nAssignmentTail  = "assignmentTail"
	nOr              = "or"
	nAnd             = "and"
	nCall            = "call"
	nCallTail        = "callTail"
	nArguments       = "arguments"
	nArgumentsTail   = "argumentsTail"
)

const (
	expectExpr = "Expect expression."
	expectEnd  = "Expect end of expression."
	expectStmt = "Expect a statement."
)

// loxGrammar is the core program grammar and chapter 10 precedence ladder in LL
// form. Recoverable blocks, compound control statements, and function
// declarations are orchestrated in parser.go; their leaf statements and all
// expressions still run here. Two
// rewrites turn the recursive-descent expression grammar into this one:
//
//	term → term ( "-" | "+" ) factor        left recursion — an LL parser
//	                                        would recurse forever on it
//	term → factor termTail                  the standard fix: consume one
//	termTail → "-" factor termTail | ε      operand, then loop in a tail rule
//
// and every `( a | b )` alternation becomes one production per alternative,
// because prediction picks a whole production, not a branch inside one. That
// desugaring is exactly docs/grammar.md's challenge 1, done for real.
//
// The tail rewrite would normally cost associativity — a tail that builds its
// node after recursing gives `1 - 2 - 3` as (- 1 (- 2 3)), which is wrong. The
// action position is what saves it; see level().
func loxGrammar() *grammar {
	g := newGrammar()

	g.add(nProgram, nt(nDeclarations))
	g.add(nDeclarations, nt(nDeclaration), nt(nDeclarations), act(prependStatement))
	g.add(nDeclarations, act(emptyStatements)) // ε

	g.add(nDeclaration, nt(nVarDeclaration))
	g.add(nDeclaration, nt(nStatement))

	g.add(nStatement, nt(nExprStatement))
	g.add(nStatement, nt(nPrintStatement))
	g.add(nStatement, nt(nReturnStatement))
	g.add(nStatement, nt(nBlock))

	g.add(nVarDeclaration,
		term(token.VAR, ""),
		term(token.IDENTIFIER, "Expect variable name."),
		nt(nVarInitializer),
		term(token.SEMICOLON, "Expect ';' after variable declaration."),
		act(mkVarStatement),
	)
	g.add(nVarInitializer,
		term(token.EQUAL, ""),
		nt(nExpression),
		act(mkInitializer),
	)
	g.add(nVarInitializer, act(noInitializer)) // ε

	g.add(nPrintStatement,
		term(token.PRINT, ""),
		nt(nExpression),
		term(token.SEMICOLON, "Expect ';' after value."),
		act(mkPrintStatement),
	)
	g.add(nReturnStatement,
		term(token.RETURN, ""),
		nt(nReturnValue),
		term(token.SEMICOLON, "Expect ';' after return value."),
		act(mkReturnStatement),
	)
	g.add(nReturnValue, nt(nExpression), act(mkReturnValue))
	g.add(nReturnValue, act(noReturnValue)) // ε

	g.add(nExprStatement,
		nt(nExpression),
		term(token.SEMICOLON, "Expect ';' after expression."),
		act(mkExpressionStatement),
	)

	g.add(nBlock,
		term(token.LEFT_BRACE, ""),
		nt(nDeclarations),
		term(token.RIGHT_BRACE, "Expect '}' after block."),
		act(mkBlockStatement),
	)

	// Factoring the optional assignment tail keeps the grammar LL(1). The
	// semantic action validates the already-parsed left side as an l-value.
	g.add(nExpression, nt(nAssignment))
	g.add(nAssignment, nt(nOr), nt(nAssignmentTail))
	g.add(nAssignmentTail,
		term(token.EQUAL, ""),
		nt(nAssignment),
		act(mkAssign),
	)
	g.add(nAssignmentTail) // ε

	logicalLevel(g, nOr, nAnd, token.OR)
	logicalLevel(g, nAnd, "equality", token.AND)
	level(g, "equality", "comparison", token.BANG_EQUAL, token.EQUAL_EQUAL)
	level(g, "comparison", "term", token.GREATER, token.GREATER_EQUAL, token.LESS, token.LESS_EQUAL)
	level(g, "term", "factor", token.MINUS, token.PLUS)
	level(g, "factor", "unary", token.SLASH, token.STAR)

	// unary → ( "!" | "-" ) unary | call. Right-recursive, so !!nil nests
	// the way the source reads and no tail rule is needed.
	g.add("unary", term(token.BANG, ""), nt("unary"), act(mkUnary))
	g.add("unary", term(token.MINUS, ""), nt("unary"), act(mkUnary))
	g.add("unary", nt(nCall))

	// Calls are postfix operators and bind more tightly than unary operators.
	// Folding before recurring keeps factory()(1) associated from the left.
	g.add(nCall, nt("primary"), nt(nCallTail))
	g.add(nCallTail,
		term(token.LEFT_PAREN, ""),
		nt(nArguments),
		term(token.RIGHT_PAREN, "Expect ')' after arguments."),
		act(mkCall),
		nt(nCallTail),
	)
	// A property access is a second postfix suffix on the same tail rule, which
	// is what makes it bind exactly as tightly as a call. Prediction stays
	// LL(1): the three callTail productions begin with '(', '.', and ε.
	g.add(nCallTail,
		term(token.DOT, ""),
		term(token.IDENTIFIER, "Expect property name after '.'."),
		act(mkGet),
		nt(nCallTail),
	)
	g.add(nCallTail) // ε
	g.add(nArguments, nt(nExpression), act(startArguments), nt(nArgumentsTail))
	g.add(nArguments, act(emptyArguments)) // ε
	g.add(nArgumentsTail,
		term(token.COMMA, ""),
		nt(nExpression),
		act(appendArgument),
		nt(nArgumentsTail),
	)
	g.add(nArgumentsTail) // ε

	g.add("primary", term(token.FALSE, ""), act(mkConst(false)))
	g.add("primary", term(token.TRUE, ""), act(mkConst(true)))
	g.add("primary", term(token.NIL, ""), act(mkConst(nil)))
	g.add("primary", term(token.NUMBER, ""), act(mkLiteral))
	g.add("primary", term(token.STRING, ""), act(mkLiteral))
	g.add("primary", term(token.THIS, ""), act(mkThis))
	// `super` is the one primary that is not a single terminal: the '.' and the
	// method name are part of the expression rather than a callTail suffix, so
	// `super` alone can never reach the tail and be read as a value.
	// Prediction stays LL(1) — no other primary production starts with SUPER.
	g.add("primary",
		term(token.SUPER, ""),
		term(token.DOT, "Expect '.' after 'super'."),
		term(token.IDENTIFIER, "Expect superclass method name."),
		act(mkSuper),
	)
	g.add("primary", term(token.IDENTIFIER, ""), act(mkVariable))
	g.add("primary",
		term(token.LEFT_PAREN, ""),
		nt(nExpression),
		term(token.RIGHT_PAREN, "Expect ')' after expression."),
		act(mkGrouping),
	)

	for _, name := range []string{nExpression, nAssignment, nOr, nAnd, "equality", "comparison", "term", "factor", "unary", nCall, "primary"} {
		g.fail[name] = expectExpr
	}
	g.fail[nCallTail] = expectEnd
	// Both optional rules can only fail to predict when the thing they are
	// optional *within* is unfinished, so each borrows that statement's message
	// rather than falling back to the generated "Expect one of ..." list.
	g.fail[nVarInitializer] = "Expect ';' after variable declaration."
	g.fail[nReturnValue] = expectExpr
	g.fail[nProgram] = expectStmt
	g.fail[nDeclarations] = expectStmt
	g.fail[nDeclaration] = expectStmt
	g.fail[nStatement] = expectStmt

	g.entry(nProgram, expectStmt)
	g.entry(nVarDeclaration, expectStmt)
	g.entry(nPrintStatement, expectStmt)
	g.entry(nReturnStatement, expectStmt)
	g.entry(nExprStatement, expectExpr)
	g.entry(nExpression, expectExpr)

	return g
}

// level writes one rung of the precedence ladder: the rule itself and its tail.
//
//	name     → next nameTail
//	nameTail → op next {mkBinary} nameTail    (one production per operator)
//	nameTail → ε
//
// mkBinary sits *before* the recursive nameTail, and that placement is the
// whole trick. By the time it runs, the value stack holds
// [left, operator, right] for the operator just read, so it folds them into one
// node and leaves it as the left operand of the next iteration:
//
//	1 - 2 - 3  →  [1] → [1,-,2] → [(- 1 2)] → [(- 1 2),-,3] → [(- (- 1 2) 3)]
//
// Put the action after the recursion instead and the folds happen inside out,
// which is right-associativity and the wrong answer for arithmetic.
func level(g *grammar, name, next string, ops ...token.TokenType) {
	levelWith(g, name, next, mkBinary, ops...)
}

func logicalLevel(g *grammar, name, next string, ops ...token.TokenType) {
	levelWith(g, name, next, mkLogical, ops...)
}

func levelWith(g *grammar, name, next string, fold action, ops ...token.TokenType) {
	tail := name + "Tail"

	g.add(name, nt(next), nt(tail))
	for _, op := range ops {
		g.add(tail, term(op, ""), nt(next), act(fold), nt(tail))
	}
	g.add(tail) // ε

	// A tail can only fail to predict when the next token is neither one of its
	// operators nor anything that may follow the expression — "1 2" is the
	// short example. That is the LL(k) parser noticing, one token earlier than
	// recursive descent does, that the expression is already over.
	g.fail[tail] = expectEnd
}

// ------------------------------------------------------------------ actions --

// action is a semantic action: a production body element that builds AST
// instead of matching input. Actions run in body order, so each one finds
// exactly the values the symbols to its left pushed.
type action func(*stack) error

// stack is the value stack. Every matched terminal pushes its token, every
// completed nonterminal leaves its node, and actions pop what they need. The
// table guarantees the shape, so a pop that finds the wrong type is a grammar
// bug, not bad input — hence the silent zero values rather than error paths.
type stack struct{ vals []any }

func (s *stack) push(v any) { s.vals = append(s.vals, v) }

func (s *stack) pop() any {
	if len(s.vals) == 0 {
		return nil
	}
	v := s.vals[len(s.vals)-1]
	s.vals = s.vals[:len(s.vals)-1]
	return v
}

func (s *stack) expr() ast.Expr {
	e, _ := s.pop().(ast.Expr)
	return e
}

func (s *stack) token() token.Token {
	t, _ := s.pop().(token.Token)
	return t
}

func (s *stack) stmt() ast.Stmt {
	stmt, _ := s.pop().(ast.Stmt)
	return stmt
}

func (s *stack) statements() []ast.Stmt {
	statements, _ := s.pop().([]ast.Stmt)
	return statements
}

func (s *stack) arguments() []ast.Expr {
	arguments, _ := s.pop().([]ast.Expr)
	return arguments
}

func (s *stack) reset() { s.vals = s.vals[:0] }

// discard drops a token that carries no meaning past the parse — ';' and the
// like. Without it the value stack would not empty out to a single node.
func discard(s *stack) error {
	s.pop()
	return nil
}

func mkBinary(s *stack) error {
	right := s.expr()
	op := s.token()
	left := s.expr()
	s.push(&ast.Binary{Left: left, Operator: op, Right: right})
	return nil
}

func mkLogical(s *stack) error {
	right := s.expr()
	op := s.token()
	left := s.expr()
	s.push(&ast.Logical{Left: left, Operator: op, Right: right})
	return nil
}

func mkUnary(s *stack) error {
	right := s.expr()
	op := s.token()
	s.push(&ast.Unary{Operator: op, Right: right})
	return nil
}

func emptyArguments(s *stack) error {
	s.push([]ast.Expr{})
	return nil
}

func startArguments(s *stack) error {
	s.push([]ast.Expr{s.expr()})
	return nil
}

func appendArgument(s *stack) error {
	argument := s.expr()
	comma := s.token()
	arguments := s.arguments()
	if len(arguments) >= 255 {
		errors.ErrorToken(comma, "Can't have more than 255 arguments.")
	}
	arguments = append(arguments, argument)
	s.push(arguments)
	return nil
}

func mkCall(s *stack) error {
	paren := s.token()
	arguments := s.arguments()
	s.token() // '('
	callee := s.expr()
	s.push(&ast.Call{Callee: callee, Paren: paren, Arguments: arguments})
	return nil
}

// mkGet runs inside callTail, before the recursive tail, for the same reason
// mkBinary does in level(): folding now leaves the Get as the object of the
// next suffix, so a.b.c nests from the left.
func mkGet(s *stack) error {
	name := s.token()
	s.token() // '.'
	object := s.expr()
	s.push(&ast.Get{Object: object, Name: name})
	return nil
}

func mkThis(s *stack) error {
	s.push(&ast.This{Keyword: s.token()})
	return nil
}

// mkSuper pops in reverse of the body order: the method name, the '.', then
// the keyword.
func mkSuper(s *stack) error {
	method := s.token()
	s.token() // '.'
	keyword := s.token()
	s.push(&ast.Super{Keyword: keyword, Method: method})
	return nil
}

// mkGrouping pops in reverse of the body order: ')' then the expression
// then '('.
func mkGrouping(s *stack) error {
	s.token()
	inner := s.expr()
	s.token()
	s.push(&ast.Grouping{Expression: inner})
	return nil
}

// mkLiteral takes the value the scanner already computed. NUMBER and STRING
// are the only terminals that carry one.
func mkLiteral(s *stack) error {
	s.push(&ast.Literal{Value: s.token().Literal})
	return nil
}

func mkVariable(s *stack) error {
	s.push(&ast.Variable{Name: s.token()})
	return nil
}

func mkAssign(s *stack) error {
	value := s.expr()
	equals := s.token()
	left := s.expr()
	switch target := left.(type) {
	case *ast.Variable:
		s.push(&ast.Assign{Name: target.Name, Value: value})
	case *ast.Get:
		s.push(&ast.Set{Object: target.Object, Name: target.Name, Value: value})
	default:
		return errors.ParseErrorAt(equals, "Invalid assignment target.")
	}
	return nil
}

// mkConst is for keywords whose value is the keyword: true, false, nil.
func mkConst(v any) action {
	return func(s *stack) error {
		s.token()
		s.push(&ast.Literal{Value: v})
		return nil
	}
}

type initializer struct{ expression ast.Expr }

func mkInitializer(s *stack) error {
	expr := s.expr()
	s.token() // '='
	s.push(initializer{expression: expr})
	return nil
}

func noInitializer(s *stack) error {
	s.push(initializer{})
	return nil
}

func mkVarStatement(s *stack) error {
	s.token() // ';'
	init, _ := s.pop().(initializer)
	name := s.token()
	s.token() // 'var'
	s.push(&ast.Var{Name: name, Initializer: init.expression})
	return nil
}

func mkPrintStatement(s *stack) error {
	s.token() // ';'
	expr := s.expr()
	s.token() // 'print'
	s.push(&ast.Print{Expression: expr})
	return nil
}

type returnValue struct{ expression ast.Expr }

func mkReturnValue(s *stack) error {
	s.push(returnValue{expression: s.expr()})
	return nil
}

func noReturnValue(s *stack) error {
	s.push(returnValue{})
	return nil
}

func mkReturnStatement(s *stack) error {
	s.token() // ';'
	value, _ := s.pop().(returnValue)
	keyword := s.token()
	s.push(&ast.Return{Keyword: keyword, Value: value.expression})
	return nil
}

func mkExpressionStatement(s *stack) error {
	s.token() // ';'
	s.push(&ast.Expression{Expression: s.expr()})
	return nil
}

func emptyStatements(s *stack) error {
	s.push([]ast.Stmt{})
	return nil
}

func prependStatement(s *stack) error {
	tail := s.statements()
	head := s.stmt()
	statements := make([]ast.Stmt, 0, len(tail)+1)
	statements = append(statements, head)
	statements = append(statements, tail...)
	s.push(statements)
	return nil
}

func mkBlockStatement(s *stack) error {
	s.token() // '}'
	statements := s.statements()
	s.token() // '{'
	s.push(&ast.Block{Statements: statements})
	return nil
}
