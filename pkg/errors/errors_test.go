package errors_test

import (
	"errors"
	"io"
	"os"
	"testing"

	"compiler101/lexer/token"
	loxerrors "compiler101/pkg/errors"
)

func TestReportRuntimeError(t *testing.T) {
	loxerrors.Reset()
	t.Cleanup(loxerrors.Reset)

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = write
	defer func() {
		os.Stderr = original
		_ = write.Close()
		_ = read.Close()
	}()

	runtimeErr := &loxerrors.RuntimeError{
		Token:   token.Token{Type: token.PLUS, Lexeme: "+", Line: 7},
		Message: "Operands must be numbers.",
	}
	loxerrors.ReportRuntimeError(runtimeErr)

	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stderr = original
	got, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}

	if want := "Operands must be numbers.\n[line 7]\n"; string(got) != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
	if !loxerrors.HadRuntimeError {
		t.Error("HadRuntimeError was not set")
	}
	if got, want := runtimeErr.Error(), `line 7, at "+": Operands must be numbers.`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestResetClearsBothErrorKinds(t *testing.T) {
	loxerrors.HadError = true
	loxerrors.HadRuntimeError = true
	t.Cleanup(loxerrors.Reset)

	loxerrors.Reset()

	if loxerrors.HadError || loxerrors.HadRuntimeError {
		t.Errorf("Reset left HadError=%t HadRuntimeError=%t", loxerrors.HadError, loxerrors.HadRuntimeError)
	}
}

func TestResolveErrorAtReportsAndReturns(t *testing.T) {
	loxerrors.Reset()
	t.Cleanup(loxerrors.Reset)

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = write
	defer func() { os.Stderr = original }()

	returned := loxerrors.ResolveErrorAt(
		token.Token{Type: token.RETURN, Lexeme: "return", Line: 3},
		"Can't return from top-level code.",
	)

	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stderr = original
	got, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}

	want := "[line 3] Error at 'return': Can't return from top-level code.\n"
	if string(got) != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
	if !loxerrors.HadError {
		t.Error("HadError was not set")
	}
	if loxerrors.HadRuntimeError {
		t.Error("HadRuntimeError was set; a static error is not a runtime error")
	}

	var resolveErr *loxerrors.ResolveError
	if !errors.As(returned, &resolveErr) {
		t.Fatalf("returned %T, want *errors.ResolveError", returned)
	}
	if got, want := resolveErr.Error(), `line 3, at "return": Can't return from top-level code.`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
