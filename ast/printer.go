package ast

import "strings"

// Printer renders an expression as a parenthesised, Lisp-ish string:
//
//	-123 * (45.67)   =>   (* (- 123) (group 45.67))
//
// It is the debugging tool for chapter 6: it makes precedence visible, so you
// can see that 1 + 2 * 3 parsed as (+ 1 (* 2 3)) and not (* (+ 1 2) 3).
type Printer struct{}

// Compile-time proof that Printer covers every expression node. Adding a node
// without its rendering method makes this line fail the build.
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

func (p *Printer) VisitAssignExpr(e *Assign) any {
	return "(= " + e.Name.Lexeme + " " + p.Print(e.Value) + ")"
}

func (p *Printer) VisitCallExpr(e *Call) any {
	exprs := make([]Expr, 0, len(e.Arguments)+1)
	exprs = append(exprs, e.Callee)
	exprs = append(exprs, e.Arguments...)
	return p.parenthesize("call", exprs...)
}

func (p *Printer) VisitGroupingExpr(e *Grouping) any {
	return p.parenthesize("group", e.Expression)
}

func (p *Printer) VisitLogicalExpr(e *Logical) any {
	return p.parenthesize(e.Operator.Lexeme, e.Left, e.Right)
}

func (p *Printer) VisitUnaryExpr(e *Unary) any {
	return p.parenthesize(e.Operator.Lexeme, e.Right)
}

func (p *Printer) VisitLiteralExpr(e *Literal) any {
	return Stringify(e.Value)
}

func (p *Printer) VisitVariableExpr(e *Variable) any {
	return e.Name.Lexeme
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
