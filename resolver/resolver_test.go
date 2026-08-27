package resolver_test

import (
	"bytes"
	stderrors "errors"
	"io"
	"strings"
	"testing"

	"compiler101/interpreter"
	"compiler101/lexer"
	"compiler101/parser"
	"compiler101/parser/llk"
	loxerrors "compiler101/pkg/errors"
	"compiler101/resolver"
)

// resolveSource runs the real pipeline as far as resolution. These tests cannot
// use t.Parallel(): pkg/errors keeps the had-error flag in a package global, and
// these tests assert on it.
func resolveSource(t *testing.T, source string) []error {
	t.Helper()
	statements, parseErrs := parser.New(lexer.Lex(source)).ParseProgram()
	if len(parseErrs) != 0 {
		t.Fatalf("parse %q: %v", source, parseErrs)
	}
	return resolver.Resolve(interpreter.NewWithWriter(io.Discard), statements)
}

func TestResolverReportsStaticErrors(t *testing.T) {
	defer loxerrors.Reset()

	tests := []struct {
		name    string
		source  string
		lexeme  string
		message string
	}{
		{
			name:    "redeclaration in one local scope",
			source:  `{ var a = 1; var a = 2; }`,
			lexeme:  "a",
			message: "Already a variable with this name in this scope.",
		},
		{
			name:    "duplicate parameter names share the body scope",
			source:  `fun f(a, a) {}`,
			lexeme:  "a",
			message: "Already a variable with this name in this scope.",
		},
		{
			name:    "local read inside its own initializer",
			source:  `var a = "outer"; { var a = a; }`,
			lexeme:  "a",
			message: "Can't read local variable in its own initializer.",
		},
		{
			name:    "return outside any function",
			source:  `return 1;`,
			lexeme:  "return",
			message: "Can't return from top-level code.",
		},
		{
			name:    "return in a block is still top-level",
			source:  `{ return; }`,
			lexeme:  "return",
			message: "Can't return from top-level code.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loxerrors.Reset()

			errs := resolveSource(t, tt.source)
			if len(errs) != 1 {
				t.Fatalf("errors = %v, want exactly one", errs)
			}
			var resolveErr *loxerrors.ResolveError
			if !stderrors.As(errs[0], &resolveErr) {
				t.Fatalf("error = %T %v, want *errors.ResolveError", errs[0], errs[0])
			}
			if resolveErr.Message != tt.message || resolveErr.Token.Lexeme != tt.lexeme {
				t.Errorf("error = %#v, want %q at %q", resolveErr, tt.message, tt.lexeme)
			}
			if !loxerrors.HadError {
				t.Error("HadError = false, want the error reported to the user too")
			}
		})
	}
}

func TestResolverAcceptsLegalBindings(t *testing.T) {
	defer loxerrors.Reset()

	for _, source := range []string{
		// Redeclaration is legal in the global scope on purpose: it is what
		// makes a long-lived REPL usable.
		`var a = 1; var a = 2;`,
		// Shadowing a name from an enclosing scope is not redeclaration.
		`var a = 1; { var a = 2; { var a = 3; print a; } }`,
		// The function's own name is defined before its body is resolved.
		`fun f(n) { if (n > 0) return f(n - 1); return 0; }`,
		// Nested functions, and a return from the inner one.
		`fun outer() { fun inner() { return 1; } return inner; }`,
		// A loop body is its own scope, so it may shadow the loop variable.
		`for (var i = 0; i < 1; i = i + 1) { var i = "shadow"; print i; }`,
		// Reading a global inside its own initializer is legal, if useless:
		// the rule is about locals, because globals resolve dynamically.
		`var a = a;`,
	} {
		t.Run(source, func(t *testing.T) {
			loxerrors.Reset()

			if errs := resolveSource(t, source); len(errs) != 0 {
				t.Fatalf("errors = %v, want none", errs)
			}
			if loxerrors.HadError {
				t.Error("HadError = true, want a clean resolve")
			}
		})
	}
}

// TestResolverReportsEveryErrorInOnePass documents why the resolver collects
// errors instead of unwinding like the parser: it has no ambiguity to recover
// from, so there is no reason to stop at the first one.
func TestResolverReportsEveryErrorInOnePass(t *testing.T) {
	defer loxerrors.Reset()
	loxerrors.Reset()

	errs := resolveSource(t, `
		return 1;
		{
			var a = 1;
			var a = 2;
			var b = b;
		}
	`)
	if len(errs) != 3 {
		t.Fatalf("errors = %v, want three", errs)
	}

	var got strings.Builder
	for _, err := range errs {
		got.WriteString(err.Error())
		got.WriteString("\n")
	}
	for _, want := range []string{
		"Can't return from top-level code.",
		"Already a variable with this name in this scope.",
		"Can't read local variable in its own initializer.",
	} {
		if !strings.Contains(got.String(), want) {
			t.Errorf("errors = %q, want one mentioning %q", got.String(), want)
		}
	}
}

// TestResolverIsFrontEndAgnostic is the payoff of resolving the tree rather than
// the token stream: both parsers desugar `for` into the same Block/While shape,
// so one pass serves both and neither needed a line of change.
func TestResolverIsFrontEndAgnostic(t *testing.T) {
	defer loxerrors.Reset()

	const source = `
		var a = "global";
		{
			fun showA() { print a; }
			showA();
			var a = "block";
			showA();
		}
		for (var i = 0; i < 2; i = i + 1) {
			fun show() { print i; }
			show();
		}
	`

	configs := []parser.Config{
		{Kind: parser.RecursiveDescent},
		{Kind: parser.LLK, K: 1},
		{Kind: parser.LLK, K: 2},
		{Kind: parser.LLK, K: llk.MaxK},
	}

	const want = "global\nglobal\n0\n1\n"
	for _, config := range configs {
		t.Run(string(config.Kind), func(t *testing.T) {
			loxerrors.Reset()

			frontEnd, err := parser.NewOf(config, lexer.Lex(source))
			if err != nil {
				t.Fatalf("NewOf(%+v): %v", config, err)
			}
			program, parseErrs := frontEnd.ParseProgram()
			if len(parseErrs) != 0 {
				t.Fatalf("ParseProgram: %v", parseErrs)
			}

			var out bytes.Buffer
			interp := interpreter.NewWithWriter(&out)
			if errs := resolver.Resolve(interp, program); len(errs) != 0 {
				t.Fatalf("resolve: %v", errs)
			}
			if err := interp.Execute(program); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got := out.String(); got != want {
				t.Errorf("k=%d output = %q, want %q", config.K, got, want)
			}
		})
	}
}
