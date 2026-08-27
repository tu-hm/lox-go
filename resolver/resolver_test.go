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
func resolveSource(t *testing.T, source string) ([]error, []*loxerrors.Warning) {
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

			errs, _ := resolveSource(t, tt.source)
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

			if errs, _ := resolveSource(t, source); len(errs) != 0 {
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

	errs, _ := resolveSource(t, `
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
			if errs, _ := resolver.Resolve(interp, program); len(errs) != 0 {
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

func TestResolverWarnsAboutUnusedLocals(t *testing.T) {
	defer loxerrors.Reset()

	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "declared and never mentioned",
			source: `{ var unused = 1; }`,
			want:   []string{"Local variable 'unused' is never used."},
		},
		{
			name: "written but never read: assigning to a variable is not using it",
			source: `{
				var counter = 0;
				counter = 1;
			}`,
			want: []string{"Local variable 'counter' is never used."},
		},
		{
			name:   "local function never called",
			source: `{ fun helper() { return 1; } }`,
			want:   []string{"Local function 'helper' is never used."},
		},
		{
			name:   "reported in declaration order",
			source: `{ var first = 1; var second = 2; }`,
			want: []string{
				"Local variable 'first' is never used.",
				"Local variable 'second' is never used.",
			},
		},
		{
			name:   "inner scopes close first",
			source: `{ var outerVar = 1; { var innerVar = 2; } }`,
			want: []string{
				"Local variable 'innerVar' is never used.",
				"Local variable 'outerVar' is never used.",
			},
		},
		{
			name:   "a function body is a scope like any other",
			source: `fun f() { var dead = 1; return 2; } print f();`,
			want:   []string{"Local variable 'dead' is never used."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loxerrors.Reset()

			errs, warnings := resolveSource(t, tt.source)
			if len(errs) != 0 {
				t.Fatalf("errors = %v, want none: an unused local does not stop the program", errs)
			}
			if len(warnings) != len(tt.want) {
				t.Fatalf("warnings = %v, want %d", warnings, len(tt.want))
			}
			for i, want := range tt.want {
				if warnings[i].Message != want {
					t.Errorf("warning %d = %q, want %q", i, warnings[i].Message, want)
				}
			}
			if loxerrors.HadError {
				t.Error("HadError = true; a warning must not change the exit code")
			}
		})
	}
}

func TestResolverDoesNotWarnAboutUsedOrExemptNames(t *testing.T) {
	defer loxerrors.Reset()

	for _, source := range []string{
		// Read directly.
		`{ var a = 1; print a; }`,
		// Read to initialize another local, which is itself read.
		`{ var a = 1; var b = a; print b; }`,
		// Written, but the write reads it too.
		`{ var a = 0; a = a + 1; print a; }`,
		// Read from a nested function, and the function is called.
		`{ var a = 1; fun show() { print a; } show(); }`,
		// A parameter is exempt: the caller's signature dictates it, and Lox
		// has no way to spell "deliberately unused".
		`fun f(ignored) { return 1; } print f(1);`,
		// Globals are not locals. The unused check cannot see them, because
		// another line of the REPL may still read them.
		`var neverRead = 1;`,
		// A loop variable read only by the condition and the increment.
		`{ var total = 0; for (var i = 0; i < 3; i = i + 1) total = total + 1; print total; }`,
	} {
		t.Run(source, func(t *testing.T) {
			loxerrors.Reset()

			errs, warnings := resolveSource(t, source)
			if len(errs) != 0 {
				t.Fatalf("errors = %v, want none", errs)
			}
			if len(warnings) != 0 {
				t.Errorf("warnings = %v, want none", warnings)
			}
		})
	}
}

// TestUnusedCheckIsSatisfiedBySelfReference documents a limitation rather than a
// feature. The check asks whether anything reads the name, and a recursive call
// reads it — so an unused recursive local goes unreported. Answering properly
// means asking whether the name is reachable from anything that is used, which
// is a different and much larger analysis.
func TestUnusedCheckIsSatisfiedBySelfReference(t *testing.T) {
	defer loxerrors.Reset()
	loxerrors.Reset()

	_, warnings := resolveSource(t, `{
		fun countdown(n) {
			if (n > 0) return countdown(n - 1);
			return 0;
		}
	}`)
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none: the recursive call counts as a read", warnings)
	}
}
