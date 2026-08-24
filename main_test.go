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

func TestBareExpressionDetectionIsReplOnlySyntaxChoice(t *testing.T) {
	for source, want := range map[string]bool{
		"1 + 2":        true,
		"a = 3":        true,
		"1 + 2;":       false,
		"print 1;":     false,
		"var a = 1;":   false,
		"{ print 1; }": false,
	} {
		if got := isBareExpression(lexer.Lex(source)); got != want {
			t.Errorf("isBareExpression(%q) = %t, want %t", source, got, want)
		}
	}
}
