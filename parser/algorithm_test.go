package parser

import (
	"testing"

	"compiler101/ast"
	"compiler101/lexer"
	"compiler101/parser/llk"
	"compiler101/pkg/errors"
)

// sources exercises precedence, associativity, nesting, every literal kind, and
// each way a parse can fail. Two algorithms this different agreeing on all of
// it is worth more than either one's own tests.
var sources = []string{
	"1", "nil", "true", "false", `"hello"`, "1.5",
	"1 + 2", "1 + 2 * 3", "(1 + 2) * 3", "1 - 2 - 3", "1 / 2 / 3",
	"-1", "--1", "!true", "!!nil", "-1 * -2",
	"1 < 2 == true", "2 >= 1 != false", "1 < 2 <= 3 > 4 >= 5",
	`"a" + "b" == "ab"`, "((((42))))", "(1 + 2) * (3 - 4) / (5 + 6)",
	// and the failures
	"", "*", ")", "(", "(1 + 2", "1 +", "1 + 2)", "1 2", "+;", "1 + * 2",
}

func printOf(e ast.Expr) string {
	if e == nil {
		return "<nil>"
	}
	return (&ast.Printer{}).Print(e)
}

// TestAlgorithmsAgree: same tokens in, same tree or same failure out. This is
// the test that would catch a wrong FOLLOW set or a misplaced semantic action,
// because it compares against an implementation that shares none of that code.
func TestAlgorithmsAgree(t *testing.T) {
	defer errors.Reset()

	for _, src := range sources {
		for k := llk.MinK; k <= llk.MaxK; k++ {
			errors.Reset()
			rd, err := NewOf(Config{Kind: RecursiveDescent}, lexer.Lex(src))
			if err != nil {
				t.Fatal(err)
			}
			wantExpr, wantErr := rd.Parse()

			errors.Reset()
			ll, err := NewOf(Config{Kind: LLK, K: k}, lexer.Lex(src))
			if err != nil {
				t.Fatal(err)
			}
			gotExpr, gotErr := ll.Parse()

			if (wantErr == nil) != (gotErr == nil) {
				t.Errorf("%q at k = %d: recursive descent gave (%s, %v), LL(k) gave (%s, %v)",
					src, k, printOf(wantExpr), wantErr, printOf(gotExpr), gotErr)
				continue
			}
			if wantErr != nil {
				continue // both failed; the messages may differ, see below
			}
			if got, want := printOf(gotExpr), printOf(wantExpr); got != want {
				t.Errorf("%q at k = %d:\n  rd   %s\n  llk  %s", src, k, want, got)
			}
		}
	}
}

// TestAlgorithmsAgreeOnBatches: same again through the recovering entry point,
// where the two also have to agree on how many statements survived.
func TestAlgorithmsAgreeOnBatches(t *testing.T) {
	defer errors.Reset()

	batches := []string{
		"1 + 2; 3 * 4;",
		"1 + ; 2 * ; 3 + 4;",
		"1 + 2",
		";;;",
		"(1; 2;",
		"1; 2; 3;",
		"var x; 1 + 2;",
	}

	for _, src := range batches {
		errors.Reset()
		rd, _ := NewOf(Config{Kind: RecursiveDescent}, lexer.Lex(src))
		wantExprs, wantErrs := rd.ParseAll()

		errors.Reset()
		ll, _ := NewOf(Config{Kind: LLK}, lexer.Lex(src))
		gotExprs, gotErrs := ll.ParseAll()

		if len(wantExprs) != len(gotExprs) || len(wantErrs) != len(gotErrs) {
			t.Errorf("%q: rd gave %d trees / %d errors, llk gave %d trees / %d errors",
				src, len(wantExprs), len(wantErrs), len(gotExprs), len(gotErrs))
			continue
		}
		for i := range wantExprs {
			if got, want := printOf(gotExprs[i]), printOf(wantExprs[i]); got != want {
				t.Errorf("%q [%d]:\n  rd   %s\n  llk  %s", src, i, want, got)
			}
		}
	}
}

func printStatements(statements []ast.Stmt) []string {
	printer := ast.NewStmtPrinter(&ast.Printer{})
	out := make([]string, len(statements))
	for i, statement := range statements {
		out[i] = printer.Print(statement)
	}
	return out
}

func TestAlgorithmsAgreeOnPrograms(t *testing.T) {
	defer errors.Reset()

	programs := []string{
		`print 1 + 2;`,
		`var a; var b = 2; print a; print b;`,
		`var a = 1; a = a + 2; print a;`,
		`var a; var b; a = b = 3;`,
		`{ var a = "inner"; print a; }`,
		`var global = 1; { { global = 2; } } print global;`,
		`a + b = 3; print 4;`,
		`{ print 1 + ; print 2; }`,
	}

	for _, src := range programs {
		errors.Reset()
		rd, _ := NewOf(Config{Kind: RecursiveDescent}, lexer.Lex(src))
		wantStatements, wantErrors := rd.ParseProgram()
		want := printStatements(wantStatements)

		for k := llk.MinK; k <= llk.MaxK; k++ {
			errors.Reset()
			ll, err := NewOf(Config{Kind: LLK, K: k}, lexer.Lex(src))
			if err != nil {
				t.Fatal(err)
			}
			gotStatements, gotErrors := ll.ParseProgram()
			got := printStatements(gotStatements)
			if len(gotErrors) != len(wantErrors) || len(got) != len(want) {
				t.Errorf("%q at k=%d: rd gave %v / %d errors, llk gave %v / %d errors",
					src, k, want, len(wantErrors), got, len(gotErrors))
				continue
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("%q at k=%d statement %d:\n  rd  %s\n  llk %s", src, k, i, want[i], got[i])
				}
			}
		}
	}
}

// TestErrorPositionsMatch: both parsers must stop at the same token, even
// though only the LL(k) one is required to by construction. Where they differ
// is the message, and the two cases below are the ones that do — the LL(k)
// parser notices at the tail rule that the expression is over, one rule before
// recursive descent returns and checks for leftovers.
func TestErrorPositionsMatch(t *testing.T) {
	defer errors.Reset()

	for _, src := range []string{"(1 + 2", "1 +", "1 + 2)", "*", ")", "1 2"} {
		errors.Reset()
		rd, _ := NewOf(Config{Kind: RecursiveDescent}, lexer.Lex(src))
		_, rdErr := rd.Parse()

		errors.Reset()
		ll, _ := NewOf(Config{Kind: LLK}, lexer.Lex(src))
		_, llErr := ll.Parse()

		a, ok1 := rdErr.(*ParseError)
		b, ok2 := llErr.(*ParseError)
		if !ok1 || !ok2 {
			t.Errorf("%q: rd %v, llk %v", src, rdErr, llErr)
			continue
		}
		if a.Token.Type != b.Token.Type || a.Token.Line != b.Token.Line {
			t.Errorf("%q: rd stopped at %v, llk at %v", src, a.Token, b.Token)
		}
	}
}

func TestParseKind(t *testing.T) {
	for in, want := range map[string]Kind{
		"":                  RecursiveDescent,
		"rd":                RecursiveDescent,
		"Recursive-Descent": RecursiveDescent,
		"llk":               LLK,
		"LL(k)":             LLK,
		" ll ":              LLK,
	} {
		got, err := ParseKind(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if _, err := ParseKind("earley"); err == nil {
		t.Error("accepted an algorithm that is not implemented")
	}
}

func TestNewOfRejectsBadConfig(t *testing.T) {
	tokens := lexer.Lex("1;")

	if _, err := NewOf(Config{Kind: "lalr"}, tokens); err == nil {
		t.Error("accepted an unknown kind")
	}
	if _, err := NewOf(Config{Kind: LLK, K: llk.MaxK + 1}, tokens); err == nil {
		t.Error("accepted an out-of-range k")
	}
	if _, err := NewOf(Config{Kind: RecursiveDescent, K: 2}, tokens); err == nil {
		t.Error("accepted a k for an algorithm that has none")
	}
}
