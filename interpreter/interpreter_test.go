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
	"compiler101/resolver"
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

// execute resolves, then runs, which is the order the real pipeline uses. Since
// chapter 11 the interpreter reads a local at the depth the resolver recorded,
// so executing an unresolved tree would look every local up in globals.
func execute(t *testing.T, interp *interpreter.Interpreter, program []ast.Stmt) error {
	t.Helper()
	if errs, _ := resolver.Resolve(interp, program); len(errs) != 0 {
		t.Fatalf("resolve: %v", errs)
	}
	return interp.Execute(program)
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

	if err := execute(t, interp, program); err != nil {
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

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "3\n3\n3\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestUndefinedVariableErrors(t *testing.T) {
	for _, source := range []string{"print missing;", "missing = 1;"} {
		t.Run(source, func(t *testing.T) {
			err := execute(t, interpreter.NewWithWriter(&bytes.Buffer{}), parseProgram(t, source))
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

	err := execute(t, interp, parseProgram(t, `{ var local = "inside"; print missing; }`))
	if err == nil {
		t.Fatal("failing block returned no error")
	}
	err = execute(t, interp, parseProgram(t, `print local;`))
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

	if err := execute(t, interp, program); err != nil {
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

	if err := execute(t, interp, program); err != nil {
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

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "6\n0\n1\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}

	err := execute(t, interp, parseProgram(t, "print i;"))
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
		err := execute(t, interpreter.NewWithWriter(&bytes.Buffer{}), parseProgram(t, test.source))
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

	if err := execute(t, interp, program); err != nil {
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

	if err := execute(t, interp, program); err != nil {
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

	if err := execute(t, interp, program); err != nil {
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
		if err := execute(t, interpreter.NewWithWriter(&out), program); err != nil {
			t.Fatalf("%s Execute: %v", kind, err)
		}
		outputs = append(outputs, out.String())
	}
	if outputs[0] != "42\n" || outputs[1] != outputs[0] {
		t.Errorf("recursive descent = %q, LL(k) = %q, want 42", outputs[0], outputs[1])
	}
}

// TestResolvedScopeIgnoresLaterDeclarations is the chapter's motivating program.
// showA is declared while a is still the global, and the later local a lands in
// the same block — the same runtime environment — yet must not change what showA
// reads. Before chapter 11 this printed "global" then "block".
func TestResolvedScopeIgnoresLaterDeclarations(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		var a = "global";
		{
			fun showA() { print a; }
			showA();
			var a = "block";
			showA();
		}
	`)

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "global\nglobal\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestResolvedDepthReachesTheIntendedScope exercises a distance greater than
// one: inner reads and writes a variable two environments up, past a scope that
// does not declare it, and the identically named global stays untouched.
func TestResolvedDepthReachesTheIntendedScope(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		var value = "global";
		fun outer() {
			var value = "outer";
			fun middle() {
				fun inner() {
					value = value + "!";
					return value;
				}
				return inner;
			}
			return middle();
		}

		var bump = outer();
		print bump();
		print bump();
		print value;
	`)

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "outer!\nouter!!\nglobal\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestUnresolvedLocalFallsBackToGlobals pins down what the interpreter does with
// a tree nobody resolved: locals are not found, so every name is looked up in
// globals. It is the reason resolution is a required pipeline stage rather than
// an optimization, and the reason the test helper above always runs it.
func TestUnresolvedLocalFallsBackToGlobals(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)

	err := interp.Execute(parseProgram(t, `{ var local = "inside"; print local; }`))
	var runtimeErr *loxerrors.RuntimeError
	if !stderrors.As(err, &runtimeErr) || runtimeErr.Token.Lexeme != "local" {
		t.Fatalf("unresolved program error = %v, want undefined variable 'local'", err)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want nothing", out.String())
	}
}

// TestSlotIndicesFollowDeclarationOrder exercises the assumption the slice-based
// environment rests on: the resolver numbers a scope's declarations in source
// order, and the runtime fills them in that same order.
func TestSlotIndicesFollowDeclarationOrder(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		{
			var first = "1";
			var second = "2";
			var third = "3";

			fun show() { print third + second + first; }
			fun rotate() {
				var carried = first;
				first = second;
				second = third;
				third = carried;
			}

			show();
			rotate();
			show();
		}
	`)

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "321\n132\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestEarlyReturnSkipsLaterSlots covers the one way a scope can run without
// filling every slot the resolver numbered. Nothing after the return can read
// them, so the unfilled tail is unreachable rather than wrong.
func TestEarlyReturnSkipsLaterSlots(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		fun f(early) {
			var head = "a";
			if (early) return head;
			var tail = "b";
			return head + tail;
		}

		print f(true);
		print f(false);
	`)

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "a\nab\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------- classes --

func TestClassesInstancesAndProperties(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		class Bagel {
			topping() { return "everything"; }
		}
		print Bagel;
		var bagel = Bagel();
		print bagel;

		// A field does not exist until it is assigned, and assignment is an
		// expression that evaluates to the value stored.
		print bagel.flavor = "plain";
		print bagel.flavor;
		bagel.flavor = "sesame";
		print bagel.flavor;

		// A method found through an instance is already bound to it.
		print bagel.topping();

		// A field shadows a method of the same name: the lookup order is
		// fields first, and that is a language decision, not an accident.
		bagel.topping = "shadowed";
		print bagel.topping;
	`)

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "Bagel\nBagel instance\nplain\nplain\nsesame\neverything\nshadowed\n"
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestBoundMethodOutlivesItsAccess is the reason methods are bound at access
// time rather than at call time. Once `serve` is in a variable, nothing at the
// call site says which instance it belongs to.
func TestBoundMethodOutlivesItsAccess(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		class Person {
			init(name) { this.name = name; }
			sayName() { print this.name; }
		}
		var jane = Person("Jane");
		var bill = Person("Bill");

		var method = jane.sayName;
		method();

		// The book's callback case: the same method value handed to a
		// function that knows nothing about receivers.
		fun callTwice(f) { f(); f(); }
		callTwice(bill.sayName);

		// Reassigning a field the method reads is visible through the binding,
		// because the binding captured the instance and not the value.
		jane.name = "Renamed";
		method();
	`)

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "Jane\nBill\nBill\nRenamed\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestThisResolvesThroughNestedScopes is the test that would catch a wrong slot
// or a wrong distance. `this` is a resolved local here, so a closure nested
// inside a method has to reach two environments out to find it, and a local
// declared alongside must not collide with its slot.
func TestThisResolvesThroughNestedScopes(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		class Thing {
			init() { this.name = "thing"; }
			makeReader() {
				var prefix = "read ";
				fun read() {
					var suffix = "!";
					return prefix + this.name + suffix;
				}
				return read;
			}
			sumTo(n) {
				this.total = 0;
				for (var i = 1; i <= n; i = i + 1) {
					{ this.total = this.total + i; }
				}
				return this.total;
			}
		}
		var thing = Thing();
		print thing.makeReader()();
		print thing.sumTo(4);

		// A class declared inside a function: the class name is itself a local,
		// one scope further out than "this" from inside a method body.
		fun makeInner() {
			var tag = "inner";
			class Inner {
				describe() { return tag + "/" + Inner().kind(); }
				kind() { return "class"; }
			}
			return Inner();
		}
		print makeInner().describe();

		// this is the instance, by identity, not a copy of it.
		class Self { me() { return this; } }
		var self = Self();
		print self.me() == self;
	`)

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := out.String(), "read thing!\n10\ninner/class\ntrue\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestInitializersRunAndAlwaysReturnTheInstance(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		class Point {
			init(x, y) {
				this.x = x;
				this.y = y;
			}
			sum() { return this.x + this.y; }
		}
		var point = Point(1, 2);
		print point.sum();

		// Calling init() directly re-initializes and still hands back the
		// instance rather than nil — that is what isInitializer forces.
		var same = point.init(10, 20);
		print same.sum();
		print same == point;

		// An early bare return exits the body but does not change the answer.
		class Guarded {
			init(skip) {
				this.value = "set";
				if (skip) return;
				this.value = "also set";
			}
		}
		print Guarded(true).value;
		print Guarded(false).value;

		// A class with no initializer takes no arguments and constructs fine.
		class Bare {}
		print Bare();
	`)

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "3\n30\ntrue\nset\nalso set\nBare instance\n"
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestClassRuntimeErrors(t *testing.T) {
	defer loxerrors.Reset()

	tests := []struct {
		name    string
		source  string
		message string
	}{
		{
			name:    "property read on a non-instance",
			source:  `print "not an object".field;`,
			message: "Only instances have properties.",
		},
		{
			name:    "property write on a non-instance",
			source:  `var n = 1; n.field = 2;`,
			message: "Only instances have fields.",
		},
		{
			// A class owns properties of its own since the class-methods
			// challenge, so this is no longer "only instances have properties":
			// the class is a valid owner, it just has no such property.
			name:    "an instance method is not reachable through the class",
			source:  `class C { m() { return 1; } } print C.m;`,
			message: "Undefined property 'm'.",
		},
		{
			name:    "a class method is not reachable through an instance",
			source:  `class C { class m() { return 1; } } print C().m;`,
			message: "Undefined property 'm'.",
		},
		{
			name:    "property that was never assigned and is not a method",
			source:  `class C {} print C().missing;`,
			message: "Undefined property 'missing'.",
		},
		{
			name:    "initializer arity is the class's arity",
			source:  `class C { init(a) {} } C();`,
			message: "Expected 1 arguments but got 0.",
		},
		{
			name:    "a class with no initializer takes no arguments",
			source:  `class C {} C(1);`,
			message: "Expected 0 arguments but got 1.",
		},
		{
			name:    "calling the result of a property that is not callable",
			source:  `class C {} var c = C(); c.field = 1; c.field();`,
			message: "Can only call functions and classes.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interp := interpreter.NewWithWriter(&bytes.Buffer{})
			err := execute(t, interp, parseProgram(t, tt.source))

			var runtimeErr *loxerrors.RuntimeError
			if !stderrors.As(err, &runtimeErr) {
				t.Fatalf("Execute(%q) error = %v, want a *RuntimeError", tt.source, err)
			}
			if runtimeErr.Message != tt.message {
				t.Errorf("message = %q, want %q", runtimeErr.Message, tt.message)
			}
		})
	}
}

// TestSetExprReportsTheObjectBeforeEvaluatingTheValue pins the evaluation
// order. The object is evaluated first, so a non-instance target fails before
// the right-hand side runs at all.
func TestSetExprReportsTheObjectBeforeEvaluatingTheValue(t *testing.T) {
	defer loxerrors.Reset()

	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		fun loud() { print "evaluated"; return 1; }
		var notAnInstance = "text";
		notAnInstance.field = loud();
	`)

	if err := execute(t, interp, program); err == nil {
		t.Fatal("Execute succeeded, want a runtime error")
	}
	if got := out.String(); got != "" {
		t.Errorf("output = %q, want nothing: the value was evaluated despite the bad target", got)
	}
}

func TestParserFrontEndsExecuteClassesEqually(t *testing.T) {
	const source = `
		class Counter {
			init(start) { this.count = start; }
			bump() {
				this.count = this.count + 1;
				return this.count;
			}
		}
		var counter = Counter(40);
		counter.bump();
		print counter.bump();
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
		if err := execute(t, interpreter.NewWithWriter(&out), program); err != nil {
			t.Fatalf("%s Execute: %v", kind, err)
		}
		outputs = append(outputs, out.String())
	}
	if outputs[0] != "42\n" || outputs[1] != outputs[0] {
		t.Errorf("recursive descent = %q, LL(k) = %q, want 42", outputs[0], outputs[1])
	}
}

// TestClassMethods covers challenge 1. A class is a property owner in its own
// right, which is what lets `Math.square(3)` use the same syntax and the same
// binding machinery as an instance method.
func TestClassMethods(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		class Math {
			class square(n) { return n * n; }
			// this, inside a class method, is the class -- so one class
			// method can reach another through it. That is the whole reason
			// the resolver puts class methods in the same scope as instance
			// methods.
			class cube(n) { return n * this.square(n); }
		}
		print Math.square(3);
		print Math.cube(3);
		print Math.square;

		// A class method is a value like any other, and it stays bound.
		var squarer = Math.square;
		print squarer(5);

		// Static fields fall out for free: a class owns properties, and
		// assigning one is what creates it.
		Math.name = "arithmetic";
		print Math.name;

		// The two namespaces are separate in both directions.
		class Mixed {
			init() { this.tag = "instance"; }
			onInstance() { return this.tag; }
			class onClass() { return "class"; }
		}
		print Mixed().onInstance();
		print Mixed.onClass();

		// A class method named init is not a constructor: construction happens
		// to instances, and a class method never receives one.
		class Odd {
			class init() { return "just a class method"; }
		}
		print Odd.init();
		print Odd();
	`)

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "9\n27\n<fn square>\n25\narithmetic\ninstance\nclass\njust a class method\nOdd instance\n"
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestGetterMethods covers challenge 2. A getter is the one property whose read
// is a call, and it sits inside the same fields-then-methods rule rather than
// beside it.
func TestGetterMethods(t *testing.T) {
	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	program := parseProgram(t, `
		class Circle {
			init(radius) { this.radius = radius; }
			diameter { return this.radius * 2; }
			// A getter may read another getter, and a method may read both.
			circumference { return 3 * this.diameter; }
			scaled(by) { return this.diameter * by; }
		}
		var circle = Circle(4);
		print circle.diameter;
		print circle.circumference;
		print circle.scaled(3);

		// The getter recomputes: it is a call, not a cached field.
		circle.radius = 10;
		print circle.diameter;

		// A field of the same name shadows the getter, exactly as it shadows a
		// method — the lookup order is unchanged, not special-cased.
		circle.diameter = "shadowed";
		print circle.diameter;

		// A zero-parameter method is still a method and still needs its call.
		class Both {
			asGetter { return "getter"; }
			asMethod() { return "method"; }
		}
		print Both().asGetter;
		print Both().asMethod();

		// Static getters work, and this inside one is the class.
		class Config {
			class version { return "1.0"; }
			class describe { return "config " + this.version; }
		}
		print Config.version;
		print Config.describe;
	`)

	if err := execute(t, interp, program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "8\n24\n24\n20\nshadowed\ngetter\nmethod\n1.0\nconfig 1.0\n"
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestGetterErrorsPropagate: a getter body runs during a property read, so a
// runtime error inside it surfaces at the read rather than at some later call.
func TestGetterErrorsPropagate(t *testing.T) {
	defer loxerrors.Reset()

	interp := interpreter.NewWithWriter(&bytes.Buffer{})
	err := execute(t, interp, parseProgram(t, `
		class C { bad { return 1 + "text"; } }
		print C().bad;
	`))

	var runtimeErr *loxerrors.RuntimeError
	if !stderrors.As(err, &runtimeErr) {
		t.Fatalf("error = %v, want a *RuntimeError", err)
	}
	if want := "Operands must be two numbers or two strings."; runtimeErr.Message != want {
		t.Errorf("message = %q, want %q", runtimeErr.Message, want)
	}
}
