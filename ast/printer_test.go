// Package ast_test exercises the AST through its exported API only, the same
// way the chapter 6 parser and chapter 7 interpreter will.
//
// There is no parser yet, so every tree here is built by hand. That is the
// point of the chapter: the representation has to work before anything
// produces it.
package ast_test

import (
	"testing"

	"compiler101/ast"
	"compiler101/lexer/token"
)

// ---------------------------------------------------------------- helpers --

func op(t token.TokenType, lexeme string) token.Token {
	return token.Token{Type: t, Lexeme: lexeme, Line: 1}
}

func num(v float64) ast.Expr    { return &ast.Literal{Value: v} }
func str(v string) ast.Expr     { return &ast.Literal{Value: v} }
func group(e ast.Expr) ast.Expr { return &ast.Grouping{Expression: e} }

func unary(t token.TokenType, lexeme string, right ast.Expr) ast.Expr {
	return &ast.Unary{Operator: op(t, lexeme), Right: right}
}

func binary(left ast.Expr, t token.TokenType, lexeme string, right ast.Expr) ast.Expr {
	return &ast.Binary{Left: left, Operator: op(t, lexeme), Right: right}
}

// ------------------------------------------------------------------ tests --

// TestPrinterBookExample is the tree the book builds in its own main():
//
//	-123 * (45.67)
func TestPrinterBookExample(t *testing.T) {
	t.Parallel()

	expr := &ast.Binary{
		Left: &ast.Unary{
			Operator: token.Token{Type: token.MINUS, Lexeme: "-", Line: 1},
			Right:    &ast.Literal{Value: 123.0},
		},
		Operator: token.Token{Type: token.STAR, Lexeme: "*", Line: 1},
		Right: &ast.Grouping{
			Expression: &ast.Literal{Value: 45.67},
		},
	}

	got := (&ast.Printer{}).Print(expr)
	if want := "(* (- 123) (group 45.67))"; got != want {
		t.Errorf("Print() = %q, want %q", got, want)
	}
}

func TestPrinter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		// --- literals: the only place formatting decisions live ---
		{"nil literal", &ast.Literal{Value: nil}, "nil"},
		{"string literal", str("hi"), "hi"},
		{"empty string literal", str(""), ""},
		{"integral number drops the .0", num(123), "123"},
		{"fractional number", num(45.67), "45.67"},
		{"negative number literal", num(-1.5), "-1.5"},
		{"zero", num(0), "0"},
		{"true literal", &ast.Literal{Value: true}, "true"},
		{"false literal", &ast.Literal{Value: false}, "false"},
		// The scanner only ever produces float64 and string literals, so this
		// arm is a safety net rather than a real case. Pinned so it stays one.
		{"non-Lox value falls back to %v", &ast.Literal{Value: 42}, "42"},

		// --- grouping ---
		{"grouping", group(num(1)), "(group 1)"},
		{"nested grouping", group(group(num(1))), "(group (group 1))"},

		// --- unary ---
		{"negate", unary(token.MINUS, "-", num(123)), "(- 123)"},
		{"not", unary(token.BANG, "!", &ast.Literal{Value: true}), "(! true)"},
		{"double negate", unary(token.MINUS, "-", unary(token.MINUS, "-", num(1))), "(- (- 1))"},

		// --- binary ---
		{
			"simple binary",
			binary(num(1), token.PLUS, "+", num(2)),
			"(+ 1 2)",
		},
		{
			"logical expression",
			&ast.Logical{
				Left:     &ast.Literal{Value: false},
				Operator: op(token.OR, "or"),
				Right:    &ast.Literal{Value: true},
			},
			"(or false true)",
		},
		{
			"function call",
			&ast.Call{
				Callee:    &ast.Variable{Name: op(token.IDENTIFIER, "sum")},
				Arguments: []ast.Expr{num(1), num(2)},
			},
			"(call sum 1 2)",
		},
		{
			"comparison",
			binary(num(1), token.LESS_EQUAL, "<=", num(2)),
			"(<= 1 2)",
		},
		{
			"string concatenation",
			binary(str("a"), token.PLUS, "+", str("b")),
			"(+ a b)",
		},

		// --- the whole reason the printer exists: precedence is visible ---
		{
			"left-associative chain: (1 - 2) - 3",
			binary(binary(num(1), token.MINUS, "-", num(2)), token.MINUS, "-", num(3)),
			"(- (- 1 2) 3)",
		},
		{
			"right-nested chain: 1 - (2 - 3)",
			binary(num(1), token.MINUS, "-", binary(num(2), token.MINUS, "-", num(3))),
			"(- 1 (- 2 3))",
		},
		{
			"1 + 2 * 3 parsed correctly",
			binary(num(1), token.PLUS, "+", binary(num(2), token.STAR, "*", num(3))),
			"(+ 1 (* 2 3))",
		},
		{
			"1 + 2 * 3 parsed wrongly — the printer shows the difference",
			binary(binary(num(1), token.PLUS, "+", num(2)), token.STAR, "*", num(3)),
			"(* (+ 1 2) 3)",
		},
		{
			"book example, assembled from helpers",
			binary(unary(token.MINUS, "-", num(123)), token.STAR, "*", group(num(45.67))),
			"(* (- 123) (group 45.67))",
		},
	}

	p := &ast.Printer{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := p.Print(tt.expr); got != tt.want {
				t.Errorf("Print() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPrinterIsAnExprVisitor pins the interface satisfaction that makes the
// compile-time exhaustiveness check work.
func TestPrinterIsAnExprVisitor(t *testing.T) {
	t.Parallel()

	var v ast.ExprVisitor = &ast.Printer{}
	if got, ok := v.VisitLiteralExpr(&ast.Literal{Value: 1.0}).(string); !ok || got != "1" {
		t.Errorf("VisitLiteralExpr() = %#v, want string %q", v.VisitLiteralExpr(&ast.Literal{Value: 1.0}), "1")
	}
}

// TestPrinterRendersClassNodes covers the chapter 12 nodes. A property name is
// spelled inline rather than recursed into, because it is not a subexpression:
// nothing evaluates it, and a visitor that treated it as one would have to
// invent an Expr to hold it.
func TestPrinterRendersClassNodes(t *testing.T) {
	t.Parallel()

	this := ast.Expr(&ast.This{Keyword: op(token.THIS, "this")})

	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{
			name: "this",
			expr: this,
			want: "this",
		},
		{
			name: "property read",
			expr: &ast.Get{Object: &ast.Variable{Name: op(token.IDENTIFIER, "egg")}, Name: op(token.IDENTIFIER, "scramble")},
			want: "(. egg scramble)",
		},
		{
			name: "property read on this",
			expr: &ast.Get{Object: this, Name: op(token.IDENTIFIER, "flavor")},
			want: "(. this flavor)",
		},
		{
			name: "chained reads nest from the left",
			expr: &ast.Get{
				Object: &ast.Get{Object: &ast.Variable{Name: op(token.IDENTIFIER, "a")}, Name: op(token.IDENTIFIER, "b")},
				Name:   op(token.IDENTIFIER, "c"),
			},
			want: "(. (. a b) c)",
		},
		{
			name: "property write",
			expr: &ast.Set{Object: this, Name: op(token.IDENTIFIER, "flavor"), Value: str("plain")},
			want: "(.= this flavor plain)",
		},
		{
			name: "property write whose value is itself a read",
			expr: &ast.Set{
				Object: this,
				Name:   op(token.IDENTIFIER, "total"),
				Value:  binary(&ast.Get{Object: this, Name: op(token.IDENTIFIER, "total")}, token.PLUS, "+", num(1)),
			},
			want: "(.= this total (+ (. this total) 1))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := (&ast.Printer{}).Print(tt.expr); got != tt.want {
				t.Errorf("Print() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestStmtPrinterRendersAClass: a method prints exactly like a function,
// because at this point in the book that is what it is — the only difference is
// where the runtime files it away.
func TestStmtPrinterRendersAClass(t *testing.T) {
	t.Parallel()

	class := &ast.Class{
		Name: op(token.IDENTIFIER, "Point"),
		Methods: []*ast.Function{
			{
				Name:   op(token.IDENTIFIER, "init"),
				Params: []token.Token{op(token.IDENTIFIER, "x")},
				Body: []ast.Stmt{&ast.Expression{Expression: &ast.Set{
					Object: &ast.This{Keyword: op(token.THIS, "this")},
					Name:   op(token.IDENTIFIER, "x"),
					Value:  &ast.Variable{Name: op(token.IDENTIFIER, "x")},
				}}},
			},
			{
				Name: op(token.IDENTIFIER, "get"),
				Body: []ast.Stmt{&ast.Return{
					Keyword: op(token.RETURN, "return"),
					Value:   &ast.Get{Object: &ast.This{Keyword: op(token.THIS, "this")}, Name: op(token.IDENTIFIER, "x")},
				}},
			},
		},
	}

	got := ast.NewStmtPrinter(&ast.Printer{}).Print(class)
	want := "(class Point (fun init (x) (block (expr (.= this x x)))) (fun get () (block (return (. this x)))))"
	if got != want {
		t.Errorf("Print() = %q, want %q", got, want)
	}

	empty := ast.NewStmtPrinter(&ast.Printer{}).Print(&ast.Class{Name: op(token.IDENTIFIER, "Empty")})
	if want := "(class Empty)"; empty != want {
		t.Errorf("empty class = %q, want %q", empty, want)
	}
}
