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
