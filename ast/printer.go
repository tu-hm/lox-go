package ast

import "strings"

// Printer renders an expression as a parenthesised, Lisp-ish string:
//
//	-123 * (45.67)   =>   (* (- 123) (group 45.67))
//
// It is the debugging tool for chapter 6: it makes precedence visible, so you
// can see that 1 + 2 * 3 parsed as (+ 1 (* 2 3)) and not (* (+ 1 2) 3).
type Printer struct{}

// Compile-time proof that Printer covers every node. When chapter 8 adds a
// node type, this line is what fails the build.
var _ ExprVisitor = (*Printer)(nil)

// Print is the typed entry point. Callers never see the any that Accept
// returns — the single assertion is contained here.
func (p *Printer) Print(e Expr) string {
	s, _ := e.Accept(p).(string)
	return s
}

func (p *Printer) VisitBinaryExpr(e *Binary) any {
	return p.parenthesize(e.Operator.Lexeme, e.Left, e.Right)
}

func (p *Printer) VisitGroupingExpr(e *Grouping) any {
	return p.parenthesize("group", e.Expression)
}

func (p *Printer) VisitUnaryExpr(e *Unary) any {
	return p.parenthesize(e.Operator.Lexeme, e.Right)
}

func (p *Printer) VisitLiteralExpr(e *Literal) any {
	return Stringify(e.Value)
}

func (p *Printer) parenthesize(name string, exprs ...Expr) string {
	var b strings.Builder
	b.WriteByte('(')
	b.WriteString(name)
	for _, e := range exprs {
		b.WriteByte(' ')
		b.WriteString(p.Print(e))
	}
	b.WriteByte(')')
	return b.String()
}
