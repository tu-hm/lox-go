package parser

import (
	"fmt"
	"strings"

	"compiler101/ast"
	"compiler101/lexer/token"
	"compiler101/parser/llk"
)

// Algorithm is what the rest of the compiler depends on: a thing that turns
// tokens into trees. Which algorithm does it is a choice, not an assumption —
// the interpreter downstream cannot tell the difference, and that is the point
// of having two.
type Algorithm interface {
	// Parse reads a single expression and requires the stream to end there.
	Parse() (ast.Expr, error)

	// ParseAll reads a batch of ';'-terminated expressions, recovering after
	// each error so one bad statement costs one message.
	ParseAll() ([]ast.Expr, []error)

	// ParseProgram reads declarations and statements through EOF, recovering at
	// declaration boundaries so callers can report more than one syntax error.
	ParseProgram() ([]ast.Stmt, []error)
}

// Both front ends satisfy it, checked here because parser/llk cannot import
// this package to check it itself.
var (
	_ Algorithm = (*Parser)(nil)
	_ Algorithm = (*llk.Parser)(nil)
)

// Kind names a parsing algorithm.
type Kind string

const (
	// RecursiveDescent is the hand-written parser in this package: a function
	// per grammar rule, decisions made as it goes. Short, fast, and free to
	// break its own rules wherever a nicer error message is worth it.
	RecursiveDescent Kind = "rd"

	// LLK is the table-driven parser in parser/llk: FIRST_k/FOLLOW_k computed
	// from the grammar, a prediction table built and checked ahead of time,
	// and a driver loop with no recursion in it.
	LLK Kind = "llk"
)

// Config selects an algorithm and its knobs.
type Config struct {
	Kind Kind

	// K is the lookahead for LLK, between llk.MinK and llk.MaxK. Zero means
	// llk.DefaultK. Recursive descent ignores it: its lookahead is whatever
	// each hand-written rule happens to peek at.
	K int
}

// NewOf builds the parser named by cfg. The error is for a bad configuration —
// an unknown kind, or a k the LL(k) table cannot be built for. Syntax errors
// come later, from Parse and ParseAll.
func NewOf(cfg Config, tokens []token.Token) (Algorithm, error) {
	switch cfg.Kind {
	case "", RecursiveDescent:
		if cfg.K != 0 {
			return nil, fmt.Errorf("parser: recursive descent has no k to set")
		}
		return New(tokens), nil

	case LLK:
		k := cfg.K
		if k == 0 {
			k = llk.DefaultK
		}
		return llk.NewK(tokens, k)

	default:
		return nil, fmt.Errorf("parser: unknown algorithm %q, want %q or %q", cfg.Kind, RecursiveDescent, LLK)
	}
}

// ParseKind maps what a user types to a Kind. The spellings are the ones people
// actually reach for on a command line.
func ParseKind(s string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "rd", "recursive", "recursive-descent", "descent":
		return RecursiveDescent, nil
	case "ll", "llk", "ll(k)", "table", "predictive":
		return LLK, nil
	default:
		return "", fmt.Errorf("parser: unknown algorithm %q, want rd or llk", s)
	}
}
