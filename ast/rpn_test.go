package ast_test

import (
	"testing"

	"compiler101/ast"
	"compiler101/lexer/token"
)

// TestRPNPrinterChallengeExample is the tree from the chapter's challenge 3:
//
//	(1 + 2) * (4 - 3)   =>   1 2 + 4 3 - *
func TestRPNPrinterChallengeExample(t *testing.T) {
	t.Parallel()

	expr := binary(
		group(binary(num(1), token.PLUS, "+", num(2))),
		token.STAR, "*",
		group(binary(num(4), token.MINUS, "-", num(3))),
	)

	got := (&ast.RPNPrinter{}).Print(expr)
	if want := "1 2 + 4 3 - *"; got != want {
		t.Errorf("RPN Print() = %q, want %q", got, want)
	}
}

func TestRPNPrinter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{"literal", num(123), "123"},
		{"nil literal", &ast.Literal{Value: nil}, "nil"},
		{"string literal", str("hi"), "hi"},
		{"simple binary", binary(num(1), token.PLUS, "+", num(2)), "1 2 +"},
		{
			"logical expression",
			&ast.Logical{
				Left:     &ast.Literal{Value: false},
				Operator: op(token.OR, "or"),
				Right:    &ast.Literal{Value: true},
			},
			"false true or",
		},
		{
			"function call records arity",
			&ast.Call{
				Callee:    &ast.Variable{Name: op(token.IDENTIFIER, "sum")},
				Arguments: []ast.Expr{num(1), num(2)},
			},
			"sum 1 2 call/2",
		},
		{
			"grouping leaves no trace",
			group(binary(num(1), token.PLUS, "+", num(2))),
			"1 2 +",
		},
		{
			"nested grouping leaves no trace either",
			group(group(num(1))),
			"1",
		},
		{
			"unary minus becomes negate, so arity stays readable",
			unary(token.MINUS, "-", num(123)),
			"123 negate",
		},
		{
			"not keeps its lexeme — Lox has no infix !",
			unary(token.BANG, "!", &ast.Literal{Value: true}),
			"true !",
		},
		{
			"book example: -123 * (45.67)",
			binary(unary(token.MINUS, "-", num(123)), token.STAR, "*", group(num(45.67))),
			"123 negate 45.67 *",
		},
		{
			"left-associative chain: (1 - 2) - 3",
			binary(binary(num(1), token.MINUS, "-", num(2)), token.MINUS, "-", num(3)),
			"1 2 - 3 -",
		},
		{
			"right-nested chain: 1 - (2 - 3) — different from the above",
			binary(num(1), token.MINUS, "-", binary(num(2), token.MINUS, "-", num(3))),
			"1 2 3 - -",
		},
		{
			"1 + 2 * 3",
			binary(num(1), token.PLUS, "+", binary(num(2), token.STAR, "*", num(3))),
			"1 2 3 * +",
		},
	}

	p := &ast.RPNPrinter{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := p.Print(tt.expr); got != tt.want {
				t.Errorf("RPN Print() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBothVisitorsOverOneTree is the chapter's thesis: two operations, one
// tree, no node type touched.
func TestBothVisitorsOverOneTree(t *testing.T) {
	t.Parallel()

	expr := binary(
		group(binary(num(1), token.PLUS, "+", num(2))),
		token.STAR, "*",
		group(binary(num(4), token.MINUS, "-", num(3))),
	)

	if got, want := (&ast.Printer{}).Print(expr), "(* (group (+ 1 2)) (group (- 4 3)))"; got != want {
		t.Errorf("Printer = %q, want %q", got, want)
	}
	if got, want := (&ast.RPNPrinter{}).Print(expr), "1 2 + 4 3 - *"; got != want {
		t.Errorf("RPNPrinter = %q, want %q", got, want)
	}
}
