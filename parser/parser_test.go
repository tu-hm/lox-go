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
