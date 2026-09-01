package main

import (
	"bytes"
	"testing"

	"compiler101/interpreter"
	"compiler101/lexer"
	"compiler101/pkg/errors"
)

func TestRunSourceReusesInterpreterAcrossReplLines(t *testing.T) {
	defer errors.Reset()

	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	runSource(`var answer = 42;`, options{}, interp, true)
	runSource(`print answer;`, options{}, interp, true)

	if got, want := out.String(), "42\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRunSourceReusesFunctionsAcrossReplLines(t *testing.T) {
	defer errors.Reset()

	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	runSource(`fun twice(value) { return value * 2; }`, options{}, interp, true)
	runSource(`print twice(21);`, options{}, interp, true)

	if got, want := out.String(), "42\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestBareExpressionDetectionIsReplOnlySyntaxChoice(t *testing.T) {
	for source, want := range map[string]bool{
		"1 + 2":            true,
		"a = 3":            true,
		"1 + 2;":           false,
		"print 1;":         false,
		"var a = 1;":       false,
		"{ print 1; }":     false,
		"if (true) {}":     false,
		"while (false) {}": false,
		"for (;;) {}":      false,
		"fun f() {}":       false,
		"return 1":         false,
		// A class declaration has no semicolon either, so the keyword is the
		// only thing that keeps a REPL line from being read as an expression.
		"class C {}":         false,
		"class C { m() {} }": false,
		// A superclass clause does not change the answer: the leading keyword
		// is still what settles it.
		"class C < B {}": false,
		// `this` is an expression, even though it will not resolve at the REPL.
		// So is `super.m()`, for the same reason: whether it resolves is a
		// later question than whether the line is an expression.
		"this":           true,
		"this.field":     true,
		"super.method":   true,
		"super.method()": true,
		"egg.scramble":   true,
	} {
		if got := isBareExpression(lexer.Lex(source)); got != want {
			t.Errorf("isBareExpression(%q) = %t, want %t", source, got, want)
		}
	}
}

func TestRunSourceRefusesToRunAfterStaticError(t *testing.T) {
	defer errors.Reset()

	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	runSource(`{ var a = 1; var a = 2; print "unreachable"; }`, options{}, interp, false)

	if out.Len() != 0 {
		t.Errorf("output = %q, want nothing: a static error must stop the program before it runs", out.String())
	}
	if !errors.HadError {
		t.Error("HadError = false, want a reported static error")
	}
	if errors.HadRuntimeError {
		t.Error("HadRuntimeError = true, want a static error instead")
	}
}

func TestReplSurvivesStaticErrorAndKeepsResolvingLines(t *testing.T) {
	defer errors.Reset()

	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	runSource(`{ return 1; }`, options{}, interp, true)
	errors.Reset() // the REPL loop clears the flag between lines

	// A fresh resolver per line, the same interpreter: block scopes work on the
	// next line even though the previous one was refused.
	runSource(`{ var a = "local"; print a; }`, options{}, interp, true)

	if got, want := out.String(), "local\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	if errors.HadError {
		t.Error("HadError = true, want the good line to resolve cleanly")
	}
}

func TestRunSourceRunsDespiteWarnings(t *testing.T) {
	defer errors.Reset()

	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	runSource(`{ var unused = 1; print "ran"; }`, options{}, interp, false)

	if got, want := out.String(), "ran\n"; got != want {
		t.Errorf("output = %q, want %q: a warning must not stop the program", got, want)
	}
	if errors.HadError {
		t.Error("HadError = true, want a warning to leave the exit code alone")
	}
}

// TestRunSourceReusesClassesAcrossReplLines: a class declared on one line has
// to still be there on the next, which is the whole reason the REPL holds one
// interpreter rather than building a fresh one per line.
func TestRunSourceReusesClassesAcrossReplLines(t *testing.T) {
	defer errors.Reset()

	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	runSource(`class Answer { init() { this.value = 42; } get() { return this.value; } }`, options{}, interp, true)
	runSource(`var answer = Answer();`, options{}, interp, true)
	runSource(`print answer.get();`, options{}, interp, true)

	if got, want := out.String(), "42\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestBareThisAtTheReplIsARuntimeError documents a real consequence of the
// expression-friendly REPL. A bare line skips resolution entirely — an
// expression cannot declare anything, so there are no local scopes — which
// means `this` is not caught as the static error it would be in a script. It
// falls through to the global lookup and fails there instead.
func TestBareThisAtTheReplIsARuntimeError(t *testing.T) {
	defer errors.Reset()

	var out bytes.Buffer
	interp := interpreter.NewWithWriter(&out)
	runSource(`this`, options{}, interp, true)

	if got := out.String(); got != "" {
		t.Errorf("output = %q, want nothing printed", got)
	}
	if !errors.HadRuntimeError {
		t.Error("bare `this` did not report a runtime error")
	}
}

// TestBareSuperAtTheReplIsARuntimeError is the sibling of the `this` case, and
// it is the only way an unresolved `super` can reach the interpreter at all: a
// script is refused by the resolver, but a bare REPL line skips resolution
// because an expression declares nothing. Before the guard in VisitSuperExpr
// this read slot 0 of the globals, which have no slots, and panicked.
func TestBareSuperAtTheReplIsARuntimeError(t *testing.T) {
	defer errors.Reset()

	for _, line := range []string{"super.method", "super.method()"} {
		errors.Reset()
		var out bytes.Buffer
		interp := interpreter.NewWithWriter(&out)
		runSource(line, options{}, interp, true)

		if got := out.String(); got != "" {
			t.Errorf("%q printed %q, want nothing", line, got)
		}
		if !errors.HadRuntimeError {
			t.Errorf("%q did not report a runtime error", line)
		}
	}
}
