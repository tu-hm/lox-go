// Package parser's tests live inside the package so they can directly pin the
// recovery boundary implemented by the unexported synchronize method.
//
// These tests must not call t.Parallel(): the parser reports through the
// package-level errors.HadError flag, which is process-global state.
package parser

import (
	"fmt"
	"strings"
	"testing"

	"compiler101/ast"
	"compiler101/lexer"
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// ---------------------------------------------------------------- helpers --

// parserFor lexes src with a clean error state.
func parserFor(t *testing.T, src string) *Parser {
	t.Helper()
	errors.Reset()
	return New(lexer.Lex(src))
}

// parse runs a full parse and renders the tree, so expectations read as the
// parenthesised form the printer produces.
func parse(t *testing.T, src string) (string, error) {
	t.Helper()
	expr, err := parserFor(t, src).Parse()
	if err != nil {
		return "", err
	}
	return (&ast.Printer{}).Print(expr), nil
}

// ------------------------------------------------------------ synchronize --

// TestSynchronizeStopsAfterSemicolon: a semicolon ends a statement, so whatever
// follows it is a fresh start and the parser stops discarding there.
func TestSynchronizeStopsAfterSemicolon(t *testing.T) {
	p := parserFor(t, "1 + ; 2")
	p.Current = 2 // the ';' — where "Expect expression." would fire

	p.synchronize()

	if got := p.peek(); got.Type != token.NUMBER || got.Literal != 2.0 {
		t.Errorf("stopped at %v, want the NUMBER after the ';'", got)
	}
}

// TestSynchronizeStopsAtStatementKeyword is the regression test for the Java
// switch fallthrough: in Go, `case CLASS:` with an empty body breaks instead of
// falling into the next case, so writing the eight keywords as separate cases
// leaves only RETURN working. Every keyword in the list must stop the scan.
func TestSynchronizeStopsAtStatementKeyword(t *testing.T) {
	keywords := map[string]token.TokenType{
		"class":  token.CLASS,
		"fun":    token.FUN,
		"var":    token.VAR,
		"for":    token.FOR,
		"if":     token.IF,
		"while":  token.WHILE,
		"print":  token.PRINT,
		"return": token.RETURN,
	}

	for lexeme, want := range keywords {
		// The '*' is the offending token; synchronize always discards the token
		// it starts on, so the keyword has to sit after it to be reachable.
		p := parserFor(t, "1 + * "+lexeme+" x")
		p.Current = 2 // the '*'

		p.synchronize()

		if got := p.peek().Type; got != want {
			t.Errorf("%q: stopped at %s, want %s", lexeme, got, want)
		}
	}
}

// TestSynchronizeRunsToEndWithoutABoundary: no semicolon and no keyword means
// there is no plausible resume point, so everything left is discarded.
func TestSynchronizeRunsToEndWithoutABoundary(t *testing.T) {
	p := parserFor(t, "1 + * 2 3")
	p.Current = 2 // the '*'

	p.synchronize()

	if !p.isAtEnd() {
		t.Errorf("stopped at %v, want EOF", p.peek())
	}
}

// TestSynchronizeAlwaysMakesProgress is the property chapter 8's parse loop
// will depend on: if synchronize could return without consuming anything, a
// `for !isAtEnd() { ...; synchronize() }` loop would spin forever on a token it
// cannot parse.
func TestSynchronizeAlwaysMakesProgress(t *testing.T) {
	for _, src := range []string{")", "; ;", "* var", "1 2 3"} {
		p := parserFor(t, src)
		before := p.Current

		p.synchronize()

		if p.Current <= before && !p.isAtEnd() {
			t.Errorf("%q: current stuck at %d without reaching EOF", src, before)
		}
	}
}

// TestSynchronizeAtEndIsSafe: called on an exhausted stream — or on a Parser
// nobody initialised — it must return rather than walk off the slice.
func TestSynchronizeAtEndIsSafe(t *testing.T) {
	p := parserFor(t, "")
	p.synchronize()
	if !p.isAtEnd() {
		t.Errorf("stopped at %v, want EOF", p.peek())
	}

	(&Parser{}).synchronize() // must not panic
}

// ------------------------------------------------------------------ parse --

func TestParse(t *testing.T) {
	cases := map[string]string{
		"1 + 2 * 3":       "(+ 1 (* 2 3))",
		"1 - 2 * 3":       "(- 1 (* 2 3))",
		"1 - 2 - 3":       "(- (- 1 2) 3)",
		"1 < 2 == true":   "(== (< 1 2) true)",
		"2 >= 1 != false": "(!= (>= 2 1) false)",
		"-123 * (45.67)":  "(* (- 123) (group 45.67))",
		"!!nil":           "(! (! nil))",
	}

	for src, want := range cases {
		got, err := parse(t, src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if got != want {
			t.Errorf("%q:\n  got  %s\n  want %s", src, got, want)
		}
		if errors.HadError {
			t.Errorf("%q: HadError set on a clean parse", src)
		}
	}
	errors.Reset()
}

// TestParseErrors covers each way a parse can fail: an unclosed group, a
// missing operand, and input that continues past the end of the expression.
func TestParseErrors(t *testing.T) {
	defer errors.Reset()

	for _, src := range []string{"(1 + 2", "1 +", "1 + 2)", "", "*", "1 2", ")"} {
		_, err := parse(t, src)
		if err == nil {
			t.Errorf("%q: parsed without error", src)
			continue
		}
		if _, ok := err.(*ParseError); !ok {
			t.Errorf("%q: error is %T, want *ParseError", src, err)
		}
		if !errors.HadError {
			t.Errorf("%q: error returned but HadError not set", src)
		}
	}
}

// --------------------------------------------------------------- parseAll --

// render projects a parsed batch down to printed trees, so expectations read
// as the parenthesised form rather than as node structs.
func render(exprs []ast.Expr) []string {
	out := make([]string, len(exprs))
	for i, e := range exprs {
		out[i] = (&ast.Printer{}).Print(e)
	}
	return out
}

func TestParseAll(t *testing.T) {
	defer errors.Reset()

	exprs, errs := parserFor(t, "1 + 2; 3 * 4;").ParseAll()

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	got := render(exprs)
	want := []string{"(+ 1 2)", "(* 3 4)"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestParseAllRecovers is the point of the whole exercise: two separate syntax
// errors are both reported, and the one good expression after them still makes
// it into the batch. Without synchronize the loop would report the first error
// and then produce a cascade of nonsense from the same bad token.
func TestParseAllRecovers(t *testing.T) {
	defer errors.Reset()

	exprs, errs := parserFor(t, "1 + ; 2 * ; 3 + 4;").ParseAll()

	if len(errs) != 2 {
		t.Errorf("got %d errors, want 2: %v", len(errs), errs)
	}
	got := render(exprs)
	if len(got) != 1 || got[0] != "(+ 3 4)" {
		t.Errorf("got %v, want [(+ 3 4)]", got)
	}
	if !errors.HadError {
		t.Error("HadError not set")
	}
}

// TestParseAllMissingSemicolon: the terminator is what separates one expression
// from the next, so its absence has to be an error rather than a silent stop.
func TestParseAllMissingSemicolon(t *testing.T) {
	defer errors.Reset()

	exprs, errs := parserFor(t, "1 + 2").ParseAll()

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}
	if len(exprs) != 0 {
		t.Errorf("got %v, want no expressions", render(exprs))
	}
}

// TestParseAllTerminates guards the loop itself. Every iteration must consume
// at least one token — on the error path that is synchronize's job — or input
// the parser cannot make sense of would spin here forever. A regression shows
// up as the package test timing out rather than as a failure, so these inputs
// are deliberately the nastiest ones: nothing but unparseable tokens, and
// tokens synchronize treats as boundaries.
func TestParseAllTerminates(t *testing.T) {
	defer errors.Reset()

	for _, src := range []string{")))", ";;;", "* * *", "1 2 3", "+;+;+;", "", ";"} {
		exprs, errs := parserFor(t, src).ParseAll()
		if len(exprs) == 0 && len(errs) == 0 && src != "" {
			t.Errorf("%q: parsed to nothing at all, expected an error", src)
		}
	}
}

// ------------------------------------------------------------ parseProgram --

func renderStatements(statements []ast.Stmt) []string {
	printer := ast.NewStmtPrinter(&ast.Printer{})
	out := make([]string, len(statements))
	for i, statement := range statements {
		out[i] = printer.Print(statement)
	}
	return out
}

func TestParseProgram(t *testing.T) {
	defer errors.Reset()

	statements, errs := parserFor(t, `
		var a = 1;
		print a;
		a = a + 1;
		{ var a; print a; }
	`).ParseProgram()
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	got := renderStatements(statements)
	want := []string{
		"(var a 1)",
		"(print a)",
		"(expr (= a (+ a 1)))",
		"(block (var a) (print a))",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestAssignmentIsRightAssociative(t *testing.T) {
	defer errors.Reset()

	statements, errs := parserFor(t, "a = b = 3;").ParseProgram()
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if got := renderStatements(statements); len(got) != 1 || got[0] != "(expr (= a (= b 3)))" {
		t.Errorf("got %v, want [(expr (= a (= b 3)))]", got)
	}
}

func TestInvalidAssignmentTarget(t *testing.T) {
	defer errors.Reset()

	statements, errs := parserFor(t, "a + b = 3; print 4;").ParseProgram()
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}
	pe, ok := errs[0].(*ParseError)
	if !ok || pe.Token.Type != token.EQUAL || pe.Message != "Invalid assignment target." {
		t.Errorf("error = %v, want invalid target at '='", errs[0])
	}
	if got := renderStatements(statements); len(got) != 1 || got[0] != "(print 4)" {
		t.Errorf("recovered as %v, want [(print 4)]", got)
	}
}

func TestParseProgramRecoversInsideBlock(t *testing.T) {
	defer errors.Reset()

	statements, errs := parserFor(t, "{ print 1 + ; print 2; }").ParseProgram()
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}
	if got := renderStatements(statements); len(got) != 1 || got[0] != "(block (print 2))" {
		t.Errorf("recovered as %v, want [(block (print 2))]", got)
	}
}

func TestLogicalPrecedenceAndAssociativity(t *testing.T) {
	defer errors.Reset()

	for source, want := range map[string]string{
		"false or true and false": "(or false (and true false))",
		"true or false or nil":    "(or (or true false) nil)",
		"true and false == false": "(and true (== false false))",
	} {
		expr, err := parserFor(t, source).Parse()
		if err != nil {
			t.Fatalf("Parse(%q): %v", source, err)
		}
		if got := (&ast.Printer{}).Print(expr); got != want {
			t.Errorf("Parse(%q) = %s, want %s", source, got, want)
		}
	}
}

func TestParseControlFlowAndDesugarFor(t *testing.T) {
	defer errors.Reset()

	tests := []struct {
		source string
		want   string
	}{
		{
			"if (true) if (false) print 1; else print 2;",
			"(if true (if false (print 1) (print 2)))",
		},
		{
			"while (i < 3) i = i + 1;",
			"(while (< i 3) (expr (= i (+ i 1))))",
		},
		{
			"for (var i = 0; i < 3; i = i + 1) print i;",
			"(block (var i 0) (while (< i 3) (block (print i) (expr (= i (+ i 1))))))",
		},
		{
			"for (;;) print 1;",
			"(while true (print 1))",
		},
		{
			"for (i = 0; i < 2;) print i;",
			"(block (expr (= i 0)) (while (< i 2) (print i)))",
		},
	}

	for _, tt := range tests {
		statements, errs := parserFor(t, tt.source).ParseProgram()
		if len(errs) != 0 {
			t.Fatalf("ParseProgram(%q): %v", tt.source, errs)
		}
		got := renderStatements(statements)
		if len(got) != 1 || got[0] != tt.want {
			t.Errorf("ParseProgram(%q) = %v, want [%s]", tt.source, got, tt.want)
		}
	}
}

func TestControlFlowParseErrors(t *testing.T) {
	defer errors.Reset()

	for _, source := range []string{
		"if true) print 1;",
		"if (true print 1;",
		"while true) print 1;",
		"for (var i = 0 i < 2; i = i + 1) print i;",
		"if (true) var a = 1;",
	} {
		_, errs := parserFor(t, source).ParseProgram()
		if len(errs) == 0 {
			t.Errorf("ParseProgram(%q) returned no error", source)
		}
	}
}

func TestParseFunctionsCallsAndReturns(t *testing.T) {
	defer errors.Reset()

	for source, want := range map[string]string{
		"clock()":          "(call clock)",
		"sum(1, 2)":        "(call sum 1 2)",
		"-factory()(1, 2)": "(- (call (call factory) 1 2))",
	} {
		expr, err := parserFor(t, source).Parse()
		if err != nil {
			t.Fatalf("Parse(%q): %v", source, err)
		}
		if got := (&ast.Printer{}).Print(expr); got != want {
			t.Errorf("Parse(%q) = %s, want %s", source, got, want)
		}
	}

	statements, errs := parserFor(t, `
		fun add(a, b) {
			if (a == 0) return;
			return a + b;
		}
	`).ParseProgram()
	if len(errs) != 0 {
		t.Fatalf("ParseProgram: %v", errs)
	}
	want := "(fun add (a b) (block (if (== a 0) (return)) (return (+ a b))))"
	if got := renderStatements(statements); len(got) != 1 || got[0] != want {
		t.Errorf("ParseProgram = %v, want [%s]", got, want)
	}
}

func TestFunctionParseErrors(t *testing.T) {
	defer errors.Reset()

	for source, want := range map[string]string{
		"fun () {}":       "Expect function name.",
		"fun f {}":        "Expect '(' after function name.",
		"fun f(a,) {}":    "Expect parameter name.",
		"fun f(a {}":      "Expect ')' after parameters.",
		"fun f() return;": "Expect '{' before function body.",
		"return 1":        "Expect ';' after return value.",
	} {
		_, errs := parserFor(t, source).ParseProgram()
		if len(errs) == 0 {
			t.Errorf("ParseProgram(%q) returned no error", source)
			continue
		}
		if parseErr, ok := errs[0].(*ParseError); !ok || parseErr.Message != want {
			t.Errorf("ParseProgram(%q) error = %v, want %q", source, errs[0], want)
		}
	}
}

func TestFunctionParameterAndArgumentLimits(t *testing.T) {
	defer errors.Reset()

	params := make([]string, 256)
	arguments := make([]string, 256)
	for index := range params {
		params[index] = fmt.Sprintf("p%d", index)
		arguments[index] = "nil"
	}

	parserFor(t, "fun tooMany("+strings.Join(params, ",")+") {}").ParseProgram()
	if !errors.HadError {
		t.Error("256 parameters did not report an error")
	}

	errors.Reset()
	p := New(lexer.Lex("tooMany(" + strings.Join(arguments, ",") + ")"))
	if _, err := p.Parse(); err != nil {
		t.Fatalf("too-many-argument syntax should remain parseable: %v", err)
	}
	if !errors.HadError {
		t.Error("256 arguments did not report an error")
	}
}

// ---------------------------------------------------------------- classes --

func TestParseClassesPropertiesAndThis(t *testing.T) {
	defer errors.Reset()

	// Property access is postfix and binds exactly as tightly as a call, which
	// is what lets the two interleave without parentheses.
	for source, want := range map[string]string{
		"egg.scramble":         "(. egg scramble)",
		"a.b.c":                "(. (. a b) c)",
		"egg.scramble(3).with": "(. (call (. egg scramble) 3) with)",
		"this.field":           "(. this field)",
		"this":                 "this",
		"point.x = 1":          "(.= point x 1)",
		"a.b.c = d.e":          "(.= (. a b) c (. d e))",
		// A property assignment is right-associative for the same reason a
		// variable assignment is: the tail recurses into assignment.
		"a.x = b.y = 1": "(.= a x (.= b y 1))",
	} {
		expr, err := parserFor(t, source).Parse()
		if err != nil {
			t.Fatalf("Parse(%q): %v", source, err)
		}
		if got := (&ast.Printer{}).Print(expr); got != want {
			t.Errorf("Parse(%q) = %s, want %s", source, got, want)
		}
	}

	statements, errs := parserFor(t, `
		class Breakfast {
			init(meat) { this.meat = meat; }
			serve(who) { return "Enjoy your " + this.meat + ", " + who + "."; }
		}
	`).ParseProgram()
	if len(errs) != 0 {
		t.Fatalf("ParseProgram: %v", errs)
	}
	want := "(class Breakfast " +
		"(fun init (meat) (block (expr (.= this meat meat)))) " +
		"(fun serve (who) (block (return (+ (+ (+ (+ Enjoy your  (. this meat)) , ) who) .)))))"
	if got := renderStatements(statements); len(got) != 1 || got[0] != want {
		t.Errorf("ParseProgram = %v, want [%s]", got, want)
	}
}

// TestParseEmptyClassBody: zero methods is legal, and Methods stays nil rather
// than becoming an empty slice. Nothing downstream distinguishes the two, but a
// test pins which one the parser produces.
func TestParseEmptyClassBody(t *testing.T) {
	defer errors.Reset()

	statements, errs := parserFor(t, "class Empty {}").ParseProgram()
	if len(errs) != 0 {
		t.Fatalf("ParseProgram: %v", errs)
	}
	class, ok := statements[0].(*ast.Class)
	if !ok {
		t.Fatalf("statement = %T, want *ast.Class", statements[0])
	}
	if class.Name.Lexeme != "Empty" || len(class.Methods) != 0 {
		t.Errorf("class = %q with %d methods, want Empty with 0", class.Name.Lexeme, len(class.Methods))
	}
}

func TestClassAndPropertyParseErrors(t *testing.T) {
	defer errors.Reset()

	for source, want := range map[string]string{
		"class {}":                "Expect class name.",
		"class C":                 "Expect '{' before class body.",
		"class C {":               "Expect '}' after class body.",
		"class C { method }":      "Expect '(' after method name.",
		"class C { () {} }":       "Expect method name.",
		"class C { m() return; }": "Expect '{' before method body.",
		"egg.;":                   "Expect property name after '.'.",
		"egg.3;":                  "Expect property name after '.'.",
		// The target is a call, not a property or a variable, so there is
		// nothing to assign to.
		"egg.scramble() = 1;": "Invalid assignment target.",
	} {
		_, errs := parserFor(t, source).ParseProgram()
		if len(errs) == 0 {
			t.Errorf("ParseProgram(%q) returned no error", source)
			continue
		}
		if parseErr, ok := errs[0].(*ParseError); !ok || parseErr.Message != want {
			t.Errorf("ParseProgram(%q) error = %v, want %q", source, errs[0], want)
		}
	}
}

// TestParseClassMethods covers challenge 1's syntax. Reusing `class` as the
// modifier costs no lookahead: `class` cannot begin a method name, so one token
// settles which list the method joins.
func TestParseClassMethods(t *testing.T) {
	defer errors.Reset()

	statements, errs := parserFor(t, `
		class Math {
			class square(n) { return n * n; }
			describe() { return "math"; }
			class cube(n) { return n * n * n; }
		}
	`).ParseProgram()
	if len(errs) != 0 {
		t.Fatalf("ParseProgram: %v", errs)
	}

	class, ok := statements[0].(*ast.Class)
	if !ok {
		t.Fatalf("statement = %T, want *ast.Class", statements[0])
	}
	if len(class.Methods) != 1 || class.Methods[0].Name.Lexeme != "describe" {
		t.Errorf("Methods = %v, want just describe", class.Methods)
	}
	if len(class.ClassMethods) != 2 {
		t.Fatalf("ClassMethods has %d entries, want 2", len(class.ClassMethods))
	}
	if class.ClassMethods[0].Name.Lexeme != "square" || class.ClassMethods[1].Name.Lexeme != "cube" {
		t.Errorf("ClassMethods = %v, want square then cube", class.ClassMethods)
	}

	// The printer distinguishes the two, which is the only place the tree shows
	// which list a method landed in.
	// A class method composes both tags: "static" plus whatever the member
	// already was, so a static getter reads "(static get ...)".
	want := "(class Math (fun describe () (block (return math))) " +
		"(static fun square (n) (block (return (* n n)))) " +
		"(static fun cube (n) (block (return (* (* n n) n)))))"
	if got := renderStatements(statements); got[0] != want {
		t.Errorf("render =\n  %s\nwant\n  %s", got[0], want)
	}
}

// TestParseGetters covers challenge 2's syntax. The parameter list is optional
// only inside a class body, so `fun f {}` stays the error it always was.
func TestParseGetters(t *testing.T) {
	defer errors.Reset()

	statements, errs := parserFor(t, `
		class Circle {
			area { return 1; }
			radius() { return 2; }
			class version { return 3; }
		}
	`).ParseProgram()
	if len(errs) != 0 {
		t.Fatalf("ParseProgram: %v", errs)
	}

	class := statements[0].(*ast.Class)
	if len(class.Methods) != 2 || !class.Methods[0].IsGetter || class.Methods[1].IsGetter {
		t.Errorf("Methods = %v, want area as a getter and radius as a method", class.Methods)
	}
	if len(class.ClassMethods) != 1 || !class.ClassMethods[0].IsGetter {
		t.Errorf("ClassMethods = %v, want version as a getter", class.ClassMethods)
	}

	want := "(class Circle (get area (block (return 1))) (fun radius () (block (return 2))) " +
		"(static get version (block (return 3))))"
	if got := renderStatements(statements); got[0] != want {
		t.Errorf("render =\n  %s\nwant\n  %s", got[0], want)
	}

	// A getter is only a class-body shape. A plain function still needs its
	// parameter list.
	if _, errs := parserFor(t, "fun f { return 1; }").ParseProgram(); len(errs) == 0 {
		t.Error("fun f { } parsed as a getter, want an error")
	} else if parseErr, ok := errs[0].(*ParseError); !ok || parseErr.Message != "Expect '(' after function name." {
		t.Errorf("error = %v, want \"Expect '(' after function name.\"", errs[0])
	}
}
