// Package interpreter walks an ast.Expr and produces a runtime value.
package interpreter

import (
	"compiler101/ast"
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

type Interpreter struct{}

var _ ast.ExprVisitor = (*Interpreter)(nil)

type runtimeSignal struct {
	err *errors.RuntimeError
}

func New() *Interpreter {
	return &Interpreter{}
}

func (i *Interpreter) Interpret(e ast.Expr) (value any, err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		sig, ok := r.(runtimeSignal)
		if !ok {
			panic(r)
		}
		value, err = nil, sig.err
	}()

	return i.evaluate(e), nil
}

func (i *Interpreter) evaluate(e ast.Expr) any {
	return e.Accept(i)
}

func (i *Interpreter) fail(t token.Token, message string) {
	panic(runtimeSignal{err: &errors.RuntimeError{Token: t, Message: message}})
}

func (i *Interpreter) VisitLiteralExpr(e *ast.Literal) any {
	return e.Value
}

func (i *Interpreter) VisitGroupingExpr(e *ast.Grouping) any {
	return i.evaluate(e.Expression)
}

func (i *Interpreter) VisitUnaryExpr(e *ast.Unary) any {
	right := i.evaluate(e.Right)

	switch e.Operator.Type {
	case token.BANG:
		return !truthy(right)
	case token.MINUS:
		return -i.number(e.Operator, right)
	}

	i.fail(e.Operator, "Unsupported unary operator.")
	return nil
}

func (i *Interpreter) VisitBinaryExpr(e *ast.Binary) any {
	left := i.evaluate(e.Left)
	right := i.evaluate(e.Right)

	switch e.Operator.Type {
	case token.MINUS:
		l, r := i.numbers(e.Operator, left, right)
		return l - r
	case token.SLASH:
		l, r := i.numbers(e.Operator, left, right)
		return l / r
	case token.STAR:
		l, r := i.numbers(e.Operator, left, right)
		return l * r
	case token.PLUS:
		switch l := left.(type) {
		case float64:
			if r, ok := right.(float64); ok {
				return l + r
			}
		case string:
			if r, ok := right.(string); ok {
				return l + r
			}
		}
		i.fail(e.Operator, "Operands must be two numbers or two strings.")
	case token.GREATER:
		l, r := i.numbers(e.Operator, left, right)
		return l > r
	case token.GREATER_EQUAL:
		l, r := i.numbers(e.Operator, left, right)
		return l >= r
	case token.LESS:
		l, r := i.numbers(e.Operator, left, right)
		return l < r
	case token.LESS_EQUAL:
		l, r := i.numbers(e.Operator, left, right)
		return l <= r
	case token.BANG_EQUAL:
		return !equal(left, right)
	case token.EQUAL_EQUAL:
		return equal(left, right)
	}

	i.fail(e.Operator, "Unsupported binary operator.")
	return nil
}

func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	default:
		return true
	}
}

func equal(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a == b
}

func (i *Interpreter) number(t token.Token, v any) float64 {
	n, ok := v.(float64)
	if !ok {
		i.fail(t, "Operand must be a number.")
	}
	return n
}

func (i *Interpreter) numbers(t token.Token, a, b any) (float64, float64) {
	l, lok := a.(float64)
	r, rok := b.(float64)
	if !lok || !rok {
		i.fail(t, "Operands must be numbers.")
	}
	return l, r
}
