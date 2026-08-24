// Package interpreter evaluates expressions and executes Lox statement trees.
package interpreter

import (
	"fmt"
	"io"
	"os"

	"compiler101/ast"
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

type Interpreter struct {
	environment *Environment
	out         io.Writer
}

var (
	_ ast.ExprVisitor = (*Interpreter)(nil)
	_ ast.StmtVisitor = (*Interpreter)(nil)
)

type runtimeSignal struct {
	err *errors.RuntimeError
}

func New() *Interpreter {
	return NewWithWriter(os.Stdout)
}

// NewWithWriter creates an interpreter whose Lox print statements write to
// out. Supplying the writer keeps language output deterministic in tests.
func NewWithWriter(out io.Writer) *Interpreter {
	if out == nil {
		out = io.Discard
	}
	return &Interpreter{environment: NewEnvironment(nil), out: out}
}

func (i *Interpreter) Interpret(e ast.Expr) (value any, err error) {
	defer recoverRuntime(&err)

	return i.evaluate(e), nil
}

// Execute runs a complete program. Statements do not produce a value; output
// is an explicit side effect of a Lox print statement.
func (i *Interpreter) Execute(statements []ast.Stmt) (err error) {
	defer recoverRuntime(&err)
	for _, statement := range statements {
		i.execute(statement)
	}
	return nil
}

func recoverRuntime(err *error) {
	r := recover()
	if r == nil {
		return
	}
	sig, ok := r.(runtimeSignal)
	if !ok {
		panic(r)
	}
	*err = sig.err
}

func (i *Interpreter) evaluate(e ast.Expr) any {
	return e.Accept(i)
}

func (i *Interpreter) execute(stmt ast.Stmt) {
	stmt.Accept(i)
}

func (i *Interpreter) fail(t token.Token, message string) {
	panic(runtimeSignal{err: &errors.RuntimeError{Token: t, Message: message}})
}

func (i *Interpreter) failWith(err *errors.RuntimeError) {
	panic(runtimeSignal{err: err})
}

func (i *Interpreter) VisitAssignExpr(e *ast.Assign) any {
	value := i.evaluate(e.Value)
	if err := i.environment.Assign(e.Name, value); err != nil {
		i.failWith(err)
	}
	return value
}

func (i *Interpreter) VisitLiteralExpr(e *ast.Literal) any {
	return e.Value
}

func (i *Interpreter) VisitVariableExpr(e *ast.Variable) any {
	value, err := i.environment.Get(e.Name)
	if err != nil {
		i.failWith(err)
	}
	return value
}

func (i *Interpreter) VisitGroupingExpr(e *ast.Grouping) any {
	return i.evaluate(e.Expression)
}

func (i *Interpreter) VisitLogicalExpr(e *ast.Logical) any {
	left := i.evaluate(e.Left)

	switch e.Operator.Type {
	case token.OR:
		if truthy(left) {
			return left
		}
	case token.AND:
		if !truthy(left) {
			return left
		}
	default:
		i.fail(e.Operator, "Unsupported logical operator.")
	}

	return i.evaluate(e.Right)
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

func (i *Interpreter) VisitBlockStmt(stmt *ast.Block) any {
	i.executeBlock(stmt.Statements, NewEnvironment(i.environment))
	return nil
}

func (i *Interpreter) VisitExpressionStmt(stmt *ast.Expression) any {
	i.evaluate(stmt.Expression)
	return nil
}

func (i *Interpreter) VisitIfStmt(stmt *ast.If) any {
	if truthy(i.evaluate(stmt.Condition)) {
		i.execute(stmt.ThenBranch)
	} else if stmt.ElseBranch != nil {
		i.execute(stmt.ElseBranch)
	}
	return nil
}

func (i *Interpreter) VisitPrintStmt(stmt *ast.Print) any {
	value := i.evaluate(stmt.Expression)
	fmt.Fprintln(i.out, ast.Stringify(value))
	return nil
}

func (i *Interpreter) VisitVarStmt(stmt *ast.Var) any {
	var value any
	if stmt.Initializer != nil {
		value = i.evaluate(stmt.Initializer)
	}
	i.environment.Define(stmt.Name.Lexeme, value)
	return nil
}

func (i *Interpreter) VisitWhileStmt(stmt *ast.While) any {
	for truthy(i.evaluate(stmt.Condition)) {
		i.execute(stmt.Body)
	}
	return nil
}

func (i *Interpreter) executeBlock(statements []ast.Stmt, environment *Environment) {
	previous := i.environment
	i.environment = environment
	defer func() { i.environment = previous }()

	for _, statement := range statements {
		i.execute(statement)
	}
}
