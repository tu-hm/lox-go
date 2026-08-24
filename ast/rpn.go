package ast

import (
	"strings"

	"compiler101/lexer/token"
)

// RPNPrinter renders an expression in reverse Polish notation:
//
//	(1 + 2) * (4 - 3)   =>   1 2 + 4 3 - *
//
// This is the chapter's argument for the visitor pattern in one file: a second
// operation over the same tree, with zero changes to any node type. Compare the
// cost of adding this against the cost of adding a node type — Option A in the
// plan makes exactly that trade.
type RPNPrinter struct{}

var _ ExprVisitor = (*RPNPrinter)(nil)

// Print is the typed entry point, same shape as Printer.Print.
func (p *RPNPrinter) Print(e Expr) string {
	s, _ := e.Accept(p).(string)
	return s
}

func (p *RPNPrinter) VisitBinaryExpr(e *Binary) any {
	return p.join(e.Left, e.Right) + " " + e.Operator.Lexeme
}

func (p *RPNPrinter) VisitAssignExpr(e *Assign) any {
	return p.Print(e.Value) + " " + e.Name.Lexeme + " ="
}

// VisitGroupingExpr drops the parentheses. That is the whole point of RPN: the
// operand order already fixes the grouping, so "(" and ")" carry no
// information. Grouping still has to exist as a node — the parser needs it to
// build the right shape — it just leaves no trace in this output.
func (p *RPNPrinter) VisitGroupingExpr(e *Grouping) any {
	return p.Print(e.Expression)
}

// VisitUnaryExpr spells unary minus "negate".
//
// RPN cannot tell arity from the operator alone: "123 -" would be
// indistinguishable from a binary subtraction with a missing operand. Prefix !
// has no infix counterpart in Lox, so it keeps its lexeme.
func (p *RPNPrinter) VisitUnaryExpr(e *Unary) any {
	name := e.Operator.Lexeme
	if e.Operator.Type == token.MINUS {
		name = "negate"
	}
	return p.Print(e.Right) + " " + name
}

func (p *RPNPrinter) VisitLiteralExpr(e *Literal) any {
	return Stringify(e.Value)
}

func (p *RPNPrinter) VisitVariableExpr(e *Variable) any {
	return e.Name.Lexeme
}

func (p *RPNPrinter) join(exprs ...Expr) string {
	parts := make([]string, 0, len(exprs))
	for _, e := range exprs {
		parts = append(parts, p.Print(e))
	}
	return strings.Join(parts, " ")
}
