package errors

import (
	"fmt"
	"os"

	"compiler101/lexer/token"
)

var (
	HadError        bool
	HadRuntimeError bool
)

// Reset clears the error state. The REPL calls it between lines so one bad
// line doesn't kill the session; tests call it so they don't see each other's
// errors. Because this state is global, tests that assert on it cannot use
// t.Parallel().
func Reset() {
	HadError = false
	HadRuntimeError = false
}

func Error(line int, message string) {
	report(line, "", message)
}

func report(line int, where string, message string) {
	fmt.Fprintf(os.Stderr, "[line %d] Error%s: %s\n", line, where, message)
	HadError = true
}

// ErrorToken reports an error at a token rather than at a bare line. The parser
// needs it because the offending lexeme ("at ')'") is most of what makes a
// syntax error readable; EOF has no lexeme to show, hence the special case.
func ErrorToken(t token.Token, message string) {
	if t.Type == token.EOF {
		report(t.Line, " at end", message)
		return
	}
	report(t.Line, " at '"+t.Lexeme+"'", message)
}

// ParseError is a syntax error located at a token. Both parser front ends —
// the recursive-descent one and the table-driven LL(k) one — return this type,
// so a caller can type-assert on a single error type without knowing which
// algorithm produced it. That is also why it lives here and not in a parser
// package: parser imports parser/llk, so the shared type has to sit below both.
type ParseError struct {
	Token   token.Token
	Message string
}

func (e *ParseError) Error() string {
	if e.Token.Type == token.EOF {
		return fmt.Sprintf("line %d, at end: %s", e.Token.Line, e.Message)
	}
	return fmt.Sprintf("line %d, at %q: %s", e.Token.Line, e.Token.Lexeme, e.Message)
}

// ParseErrorAt reports t to the user and returns the value the parser unwinds
// with. The two steps are one call because every call site wants both: the
// user needs the message now, the parser needs a value to return.
func ParseErrorAt(t token.Token, message string) error {
	ErrorToken(t, message)
	return &ParseError{Token: t, Message: message}
}

// ResolveError is a static-semantics error found by the resolver: a rule about
// bindings that the grammar cannot express, such as reading a local variable
// inside its own initializer. It mirrors ParseError because callers treat the
// two identically — report, and refuse to run the program — and only the phase
// that produced them differs.
type ResolveError struct {
	Token   token.Token
	Message string
}

// Error has no at-end case, unlike ParseError: a resolver error always names a
// declaration or a keyword the parser already accepted, so there is always a
// lexeme to show.
func (e *ResolveError) Error() string {
	return fmt.Sprintf("line %d, at %q: %s", e.Token.Line, e.Token.Lexeme, e.Message)
}

// ResolveErrorAt reports t to the user and returns the value the resolver
// collects. Unlike a parse error it is not a signal to unwind: the resolver has
// no ambiguity to recover from, so it keeps walking and reports every error in
// one pass.
func ResolveErrorAt(t token.Token, message string) error {
	ErrorToken(t, message)
	return &ResolveError{Token: t, Message: message}
}

// RuntimeError is a failure discovered while evaluating an expression. Token
// identifies the operator that failed so callers can report the source line.
type RuntimeError struct {
	Token   token.Token
	Message string
}

func (e *RuntimeError) Error() string {
	return fmt.Sprintf("line %d, at %q: %s", e.Token.Line, e.Token.Lexeme, e.Message)
}

// ReportRuntimeError prints a runtime error and records it separately from a
// syntax error because the command-line exit codes differ.
func ReportRuntimeError(e *RuntimeError) {
	fmt.Fprintf(os.Stderr, "%s\n[line %d]\n", e.Message, e.Token.Line)
	HadRuntimeError = true
}
