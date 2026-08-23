package llk

import (
	"strings"

	"compiler101/ast"
	"compiler101/lexer/token"
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

// Nonterminal names. program and statements are never started at; they exist
// so FOLLOW sets are computed against a whole program rather than against a
// single statement. Without them, FOLLOW_k(exprStatement) would be {EOF} and a
// k>1 parser would reject the token after a ';' that begins the next statement.
const (
	nProgram       = "program"
	nStatements    = "statements"
	nExprStatement = "exprStatement"
	nExpression    = "expression"
)

const (
	expectExpr = "Expect expression."
	expectEnd  = "Expect end of expression."
	expectStmt = "Expect a statement."
)

// loxGrammar is the chapter 6 precedence ladder in LL form. Two rewrites turn
// the recursive-descent grammar into this one:
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

	g.add(nProgram, nt(nStatements))
	g.add(nStatements, nt(nExprStatement), nt(nStatements))
	g.add(nStatements) // ε

	g.add(nExprStatement,
		nt(nExpression),
		term(token.SEMICOLON, "Expect ';' after expression."),
		act(discard), // the ';' is punctuation; it leaves no node behind
	)

	g.add(nExpression, nt("equality"))

	level(g, "equality", "comparison", token.BANG_EQUAL, token.EQUAL_EQUAL)
	level(g, "comparison", "term", token.GREATER, token.GREATER_EQUAL, token.LESS, token.LESS_EQUAL)
	level(g, "term", "factor", token.MINUS, token.PLUS)
	level(g, "factor", "unary", token.SLASH, token.STAR)

	// unary → ( "!" | "-" ) unary | primary. Right-recursive, so !!nil nests
	// the way the source reads and no tail rule is needed.
	g.add("unary", term(token.BANG, ""), nt("unary"), act(mkUnary))
	g.add("unary", term(token.MINUS, ""), nt("unary"), act(mkUnary))
	g.add("unary", nt("primary"))

	g.add("primary", term(token.FALSE, ""), act(mkConst(false)))
	g.add("primary", term(token.TRUE, ""), act(mkConst(true)))
	g.add("primary", term(token.NIL, ""), act(mkConst(nil)))
	g.add("primary", term(token.NUMBER, ""), act(mkLiteral))
	g.add("primary", term(token.STRING, ""), act(mkLiteral))
	g.add("primary",
		term(token.LEFT_PAREN, ""),
		nt(nExpression),
		term(token.RIGHT_PAREN, "Expect ')' after expression."),
		act(mkGrouping),
	)

	for _, name := range []string{nExpression, "equality", "comparison", "term", "factor", "unary", "primary"} {
		g.fail[name] = expectExpr
	}
	g.fail[nProgram] = expectStmt
	g.fail[nStatements] = expectStmt

	g.entry(nProgram, expectStmt)
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
	tail := name + "Tail"

	g.add(name, nt(next), nt(tail))
	for _, op := range ops {
		g.add(tail, term(op, ""), nt(next), act(mkBinary), nt(tail))
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
type action func(*stack)

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

func (s *stack) reset() { s.vals = s.vals[:0] }

// discard drops a token that carries no meaning past the parse — ';' and the
// like. Without it the value stack would not empty out to a single node.
func discard(s *stack) { s.pop() }

func mkBinary(s *stack) {
	right := s.expr()
	op := s.token()
	left := s.expr()
	s.push(&ast.Binary{Left: left, Operator: op, Right: right})
}

func mkUnary(s *stack) {
	right := s.expr()
	op := s.token()
	s.push(&ast.Unary{Operator: op, Right: right})
}

// mkGrouping pops in reverse of the body order: ')' then the expression
// then '('.
func mkGrouping(s *stack) {
	s.token()
	inner := s.expr()
	s.token()
	s.push(&ast.Grouping{Expression: inner})
}

// mkLiteral takes the value the scanner already computed. NUMBER and STRING
// are the only terminals that carry one.
func mkLiteral(s *stack) { s.push(&ast.Literal{Value: s.token().Literal}) }

// mkConst is for keywords whose value is the keyword: true, false, nil.
func mkConst(v any) action {
	return func(s *stack) {
		s.token()
		s.push(&ast.Literal{Value: v})
	}
}
