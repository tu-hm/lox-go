package interpreter_test

import (
	"bytes"
	stderrors "errors"
	"testing"

	"compiler101/ast"
	"compiler101/interpreter"
	"compiler101/lexer"
	"compiler101/lexer/token"
	"compiler101/parser"
	loxerrors "compiler101/pkg/errors"
)

func parseExpr(t *testing.T, source string) ast.Expr {
	t.Helper()
	expr, err := parser.New(lexer.Lex(source)).Parse()
	if err != nil {
		t.Fatalf("parse(%q): %v", source, err)
	}
	return expr
}

func parseProgram(t *testing.T, source string) []ast.Stmt {
	t.Helper()
	statements, errs := parser.New(lexer.Lex(source)).ParseProgram()
	if len(errs) != 0 {
		t.Fatalf("parse program: %v", errs)
	}
	return statements
}

// eval drives the complete user-facing expression pipeline.
func eval(t *testing.T, source string) (string, error) {
	t.Helper()
	value, err := interpreter.New().Interpret(parseExpr(t, source))
	return ast.Stringify(value), err
}

func TestInterpretValues(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{`1`, `1`},
		{`"hi"`, `hi`},
		{`true`, `true`},
		{`nil`, `nil`},
		{`1 + 2 * 3`, `7`},
		{`(1 + 2) * 3`, `9`},
		{`-3`, `-3`},
		{`- -3`, `3`},
		{`7 / 2`, `3.5`},
		{`8 - 3`, `5`},
		{`3 * 4`, `12`},
		{`"a" + "b"`, `ab`},
		{`!true`, `false`},
		{`!false`, `true`},
		{`!nil`, `true`},
		{`!0`, `false`},
		{`!""`, `false`},
		{`1 < 2`, `true`},
		{`2 <= 2`, `true`},
		{`3 > 4`, `false`},
		{`4 >= 4`, `true`},
		{`1 == 1`, `true`},
		{`nil == nil`, `true`},
		{`nil == false`, `false`},
		{`false == nil`, `false`},
		{`1 == "1"`, `false`},
		{`true != false`, `true`},
		// Go follows IEEE 754 here; unlike Java's Double.equals, NaN != NaN.
		{`(0 / 0) == (0 / 0)`, `false`},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			got, err := eval(t, tt.source)
			if err != nil {
				t.Fatalf("Interpret(%q): %v", tt.source, err)
			}
			if got != tt.want {
				t.Errorf("Interpret(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

func TestInterpretRuntimeErrors(t *testing.T) {
	tests := []struct {
		source  string
		message string
		line    int
	}{
		{`-"a"`, `Operand must be a number.`, 1},
		{`1 + "a"`, `Operands must be two numbers or two strings.`, 1},
		{`"a" + 1`, `Operands must be two numbers or two strings.`, 1},
		{`"a" * 2`, `Operands must be numbers.`, 1},
		{`1 < "a"`, `Operands must be numbers.`, 1},
		{"\nnil - 1", `Operands must be numbers.`, 2},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			_, err := eval(t, tt.source)
			var runtimeErr *loxerrors.RuntimeError
			if !stderrors.As(err, &runtimeErr) {
				t.Fatalf("Interpret(%q) error = %T %v, want *errors.RuntimeError", tt.source, err, err)
			}
			if runtimeErr.Message != tt.message {
				t.Errorf("message = %q, want %q", runtimeErr.Message, tt.message)
			}
			if runtimeErr.Token.Line != tt.line {
				t.Errorf("line = %d, want %d", runtimeErr.Token.Line, tt.line)
			}
		})
	}
}

func TestParserFrontEndsEvaluateEqually(t *testing.T) {
	sources := []string{
		`1 + 2 * 3`,
		`(1 + 2) * 3`,
		`- -3`,
		`"a" + "b" == "ab"`,
		`!nil == true`,
		`1 < 2 == 3 >= 3`,
	}

	for _, source := range sources {
		var values []string
		for _, kind := range []parser.Kind{parser.RecursiveDescent, parser.LLK} {
			frontEnd, err := parser.NewOf(parser.Config{Kind: kind}, lexer.Lex(source))
			if err != nil {
				t.Fatalf("NewOf(%s): %v", kind, err)
			}
			expr, err := frontEnd.Parse()
			if err != nil {
				t.Fatalf("%s Parse(%q): %v", kind, source, err)
			}
			value, err := interpreter.New().Interpret(expr)
			if err != nil {
				t.Fatalf("%s Interpret(%q): %v", kind, source, err)
			}
			values = append(values, ast.Stringify(value))
		}
		if values[0] != values[1] {
			t.Errorf("%q: recursive descent = %q, LL(k) = %q", source, values[0], values[1])
		}
	}
}

func TestInterpreterContinuesAfterRuntimeError(t *testing.T) {
	interp := interpreter.New()

	if _, err := interp.Interpret(parseExpr(t, `1 + "a"`)); err == nil {
		t.Fatal("bad expression returned no error")
	}
	value, err := interp.Interpret(parseExpr(t, `40 + 2`))
	if err != nil {
		t.Fatalf("good expression after error: %v", err)
	}
	if got := ast.Stringify(value); got != "42" {
		t.Errorf("good expression after error = %q, want 42", got)
	}
}

func TestUnsupportedOperatorsReturnRuntimeErrors(t *testing.T) {
	tests := []struct {
		expr    ast.Expr
		message string
		line    int
	}{
		{expr: &ast.Unary{
			Operator: token.Token{Type: token.PLUS, Lexeme: "+", Line: 3},
			Right:    &ast.Literal{Value: 1.0},
		}, message: "Unsupported unary operator.", line: 3},
		{expr: &ast.Binary{
			Left:     &ast.Literal{Value: 1.0},
			Operator: token.Token{Type: token.COMMA, Lexeme: ",", Line: 4},
			Right:    &ast.Literal{Value: 2.0},
		}, message: "Unsupported binary operator.", line: 4},
	}

	for _, tt := range tests {
		_, err := interpreter.New().Interpret(tt.expr)
		var runtimeErr *loxerrors.RuntimeError
		if !stderrors.As(err, &runtimeErr) {
			t.Errorf("%T returned %T %v, want *errors.RuntimeError", tt.expr, err, err)
			continue
		}
		if runtimeErr.Message != tt.message || runtimeErr.Token.Line != tt.line {
			t.Errorf("%T returned (%q, line %d), want (%q, line %d)",
				tt.expr, runtimeErr.Message, runtimeErr.Token.Line, tt.message, tt.line)
		}
	}
}

func TestInterpretDoesNotLaunderUnexpectedPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Interpret swallowed an unexpected panic")
		}
	}()

	// A malformed tree is a programming error, not a Lox runtime error.
	_, _ = interpreter.New().Interpret(&ast.Grouping{})
}

func TestExecuteStatementsVariablesAndScope(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		var a = "global";
		var unset;
		print unset;
		{
			var a = "local";
			print a;
			a = a + "!";
			print a;
		}
		print a;
	`)

	if err := interp.Execute(program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "nil\nlocal\nlocal!\nglobal\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestAssignmentReturnsValueAndAssociatesRight(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		var a;
		var b;
		print a = b = 3;
		print a;
		print b;
	`)

	if err := interp.Execute(program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "3\n3\n3\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestUndefinedVariableErrors(t *testing.T) {
	for _, source := range []string{"print missing;", "missing = 1;"} {
		t.Run(source, func(t *testing.T) {
			err := interpreter.NewWithWriter(&bytes.Buffer{}).Execute(parseProgram(t, source))
			var runtimeErr *loxerrors.RuntimeError
			if !stderrors.As(err, &runtimeErr) {
				t.Fatalf("error = %T %v, want *errors.RuntimeError", err, err)
			}
			if runtimeErr.Token.Lexeme != "missing" || runtimeErr.Message != "Undefined variable 'missing'." {
				t.Errorf("error = %#v", runtimeErr)
			}
		})
	}
}

func TestEnvironmentDistinguishesNilFromMissing(t *testing.T) {
	env := interpreter.NewEnvironment(nil)
	env.Define("present", nil)

	value, err := env.Get(token.Token{Type: token.IDENTIFIER, Lexeme: "present", Line: 1})
	if err != nil || value != nil {
		t.Errorf("defined nil = (%v, %v), want (nil, nil)", value, err)
	}
	if _, err := env.Get(token.Token{Type: token.IDENTIFIER, Lexeme: "missing", Line: 2}); err == nil {
		t.Error("missing variable returned no error")
	}
}

func TestRuntimeErrorRestoresBlockEnvironment(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)

	err := interp.Execute(parseProgram(t, `{ var local = "inside"; print missing; }`))
	if err == nil {
		t.Fatal("failing block returned no error")
	}
	err = interp.Execute(parseProgram(t, `print local;`))
	var runtimeErr *loxerrors.RuntimeError
	if !stderrors.As(err, &runtimeErr) || runtimeErr.Token.Lexeme != "local" {
		t.Fatalf("after block failure got %v, want undefined local", err)
	}
}
