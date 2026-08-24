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
		{expr: &ast.Logical{
			Left:     &ast.Literal{Value: true},
			Operator: token.Token{Type: token.COMMA, Lexeme: ",", Line: 5},
			Right:    &ast.Literal{Value: false},
		}, message: "Unsupported logical operator.", line: 5},
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

func TestLogicalOperatorsShortCircuitAndReturnOperands(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		var side = "clean";
		print "left" or (side = "or-ran");
		print nil or "right";
		print false and (side = "and-ran");
		print true and "right";
		print side;
		print true or missing;
		print false and missing;
	`)

	if err := interp.Execute(program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "left\nright\nfalse\nright\nclean\ntrue\nfalse\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestIfAndWhileControlExecution(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		if (true) print "then"; else print missing;
		if (nil) print missing; else print "else";
		if (0) print "zero is truthy";
		var i = 0;
		while (i < 3) {
			print i;
			i = i + 1;
		}
	`)

	if err := interp.Execute(program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "then\nelse\nzero is truthy\n0\n1\n2\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestForDesugaringExecutionAndInitializerScope(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		var sum = 0;
		for (var i = 1; i <= 3; i = i + 1) sum = sum + i;
		print sum;
		var j = 0;
		for (; j < 2;) {
			print j;
			j = j + 1;
		}
	`)

	if err := interp.Execute(program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "6\n0\n1\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}

	err := interp.Execute(parseProgram(t, "print i;"))
	var runtimeErr *loxerrors.RuntimeError
	if !stderrors.As(err, &runtimeErr) || runtimeErr.Token.Lexeme != "i" {
		t.Fatalf("loop initializer leaked out of its scope: %v", err)
	}
}

func TestNativeClockAndCallableErrors(t *testing.T) {
	value, err := interpreter.New().Interpret(parseExpr(t, "clock()"))
	if err != nil {
		t.Fatalf("clock(): %v", err)
	}
	seconds, ok := value.(float64)
	if !ok || seconds <= 0 {
		t.Errorf("clock() = %T %v, want a positive number", value, value)
	}

	tests := []struct {
		source  string
		message string
	}{
		{`"not callable"();`, "Can only call functions and classes."},
		{`fun one(a) {} one();`, "Expected 1 arguments but got 0."},
		{`clock(1);`, "Expected 0 arguments but got 1."},
	}
	for _, test := range tests {
		err := interpreter.NewWithWriter(&bytes.Buffer{}).Execute(parseProgram(t, test.source))
		var runtimeErr *loxerrors.RuntimeError
		if !stderrors.As(err, &runtimeErr) {
			t.Errorf("Execute(%q) error = %T %v, want *errors.RuntimeError", test.source, err, err)
			continue
		}
		if runtimeErr.Message != test.message || runtimeErr.Token.Type != token.RIGHT_PAREN {
			t.Errorf("Execute(%q) error = %#v, want %q at ')'", test.source, runtimeErr, test.message)
		}
	}
}

func TestUserFunctionsRecursionAndReturns(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		fun add(a, b) { return a + b; }
		print add;
		print add(2, 3);

		fun procedure() { print "body"; }
		print procedure();

		fun findThree(n) {
			while (n < 10) {
				if (n == 3) return n;
				n = n + 1;
			}
			return;
		}
		print findThree(1);

		fun fib(n) {
			if (n <= 1) return n;
			return fib(n - 2) + fib(n - 1);
		}
		print fib(8);
	`)

	if err := interp.Execute(program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "<fn add>\n5\nbody\nnil\n3\n21\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestArgumentsEvaluateLeftToRight(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		var trace = "";
		fun mark(value) {
			trace = trace + value;
			return value;
		}
		fun pair(a, b) { return a + b; }
		print pair(mark("a"), mark("b"));
		print trace;
	`)

	if err := interp.Execute(program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "ab\nab\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestClosuresCaptureAndRetainDeclarationEnvironment(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		fun makeCounter() {
			var i = 0;
			fun count() {
				i = i + 1;
				return i;
			}
			return count;
		}

		var first = makeCounter();
		var second = makeCounter();
		print first();
		print first();
		print second();
	`)

	if err := interp.Execute(program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "1\n2\n1\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestParserFrontEndsExecuteFunctionsEqually(t *testing.T) {
	const source = `
		fun makeAdder(a) {
			fun add(b) { return a + b; }
			return add;
		}
		var addTwo = makeAdder(2);
		print addTwo(40);
	`

	var outputs []string
	for _, kind := range []parser.Kind{parser.RecursiveDescent, parser.LLK} {
		frontEnd, err := parser.NewOf(parser.Config{Kind: kind}, lexer.Lex(source))
		if err != nil {
			t.Fatalf("NewOf(%s): %v", kind, err)
		}
		program, errs := frontEnd.ParseProgram()
		if len(errs) != 0 {
			t.Fatalf("%s ParseProgram: %v", kind, errs)
		}
		var out bytes.Buffer
		if err := interpreter.NewWithWriter(&out).Execute(program); err != nil {
			t.Fatalf("%s Execute: %v", kind, err)
		}
		outputs = append(outputs, out.String())
	}
	if outputs[0] != "42\n" || outputs[1] != outputs[0] {
		t.Errorf("recursive descent = %q, LL(k) = %q, want 42", outputs[0], outputs[1])
	}
}
