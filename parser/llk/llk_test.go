// Tests live inside the package because the interesting parts — FIRST_k,
// FOLLOW_k, the table — are unexported, and because they are what can actually
// be wrong. A parse that succeeds proves little; a FOLLOW set that quietly
// misses a terminal turns into a rejected program three rules away.
//
// No t.Parallel() anywhere: the parser reports through errors.HadError, which
// is process-global.
package llk

import (
	"strings"
	"testing"

	"compiler101/ast"
	"compiler101/lexer"
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// ---------------------------------------------------------------- helpers --

func parserFor(t *testing.T, src string, k int) *Parser {
	t.Helper()
	errors.Reset()
	p, err := NewK(lexer.Lex(src), k)
	if err != nil {
		t.Fatalf("NewK(%d): %v", k, err)
	}
	return p
}

func parse(t *testing.T, src string, k int) (string, error) {
	t.Helper()
	expr, err := parserFor(t, src, k).Parse()
	if err != nil {
		return "", err
	}
	return (&ast.Printer{}).Print(expr), nil
}

func render(exprs []ast.Expr) []string {
	out := make([]string, len(exprs))
	for i, e := range exprs {
		out[i] = (&ast.Printer{}).Print(e)
	}
	return out
}

// ------------------------------------------------------------------- sets --

// TestFirstSet: FIRST_1(expression) is every token an expression can start
// with. Anything missing here is a valid program the parser would reject.
func TestFirstSet(t *testing.T) {
	s := analyze(loxGrammar(), 1)

	want := []token.TokenType{
		token.NUMBER, token.STRING, token.TRUE, token.FALSE, token.NIL,
		token.IDENTIFIER, token.LEFT_PAREN, token.MINUS, token.BANG,
	}
	for _, tt := range want {
		if !s.first[nExpression].has(seq{tt}) {
			t.Errorf("FIRST_1(expression) missing %s", tt)
		}
	}
	if got, wantN := len(s.first[nExpression]), len(want); got != wantN {
		t.Errorf("FIRST_1(expression) has %d strings, want %d: %v", got, wantN, s.first[nExpression].sorted())
	}
}

// TestFollowSetDecidesEpsilon is the load-bearing property of the whole
// precedence ladder. termTail → ε is predicted on FOLLOW_1(termTail), so that
// set has to contain every token that can legally end a term — and must not
// contain '+' or '-', because those belong to the other productions of the
// same rule. If it did, the grammar would not be LL(1) and the table would
// refuse to build.
func TestFollowSetDecidesEpsilon(t *testing.T) {
	s := analyze(loxGrammar(), 1)
	follow := s.follow["termTail"]

	for _, tt := range []token.TokenType{
		token.SEMICOLON, token.RIGHT_PAREN, token.EOF,
		token.EQUAL_EQUAL, token.BANG_EQUAL, token.LESS, token.GREATER_EQUAL,
	} {
		if !follow.has(seq{tt}) {
			t.Errorf("FOLLOW_1(termTail) missing %s", tt)
		}
	}
	for _, tt := range []token.TokenType{token.PLUS, token.MINUS, token.NUMBER} {
		if follow.has(seq{tt}) {
			t.Errorf("FOLLOW_1(termTail) contains %s, which would make the rule ambiguous", tt)
		}
	}
}

// TestFollowSpansStatements: with k > 1 the window after a ';' reaches into the
// next statement, so FOLLOW_2(expression) has to know what a statement can
// start with. This is why the grammar carries program and statements rules it
// never parses with.
func TestFollowSpansStatements(t *testing.T) {
	s := analyze(loxGrammar(), 2)

	for _, want := range []seq{
		{token.SEMICOLON, token.NUMBER},
		{token.SEMICOLON, token.LEFT_PAREN},
		{token.SEMICOLON, token.EOF},
	} {
		if !s.follow[nExpression].has(want) {
			t.Errorf("FOLLOW_2(expression) missing %s", want)
		}
	}
}

// TestNormCutsAtEOF: nothing follows the end of the stream, so a k-string that
// reaches EOF stops there however much room is left. Without this the lookahead
// window and the FOLLOW sets would disagree about how to spell "the input ends
// here" and nothing would match.
func TestNormCutsAtEOF(t *testing.T) {
	got := norm(seq{token.NUMBER, token.EOF, token.NUMBER}, 3)
	if len(got) != 2 || got[1] != token.EOF {
		t.Errorf("norm = %s, want NUMBER EOF", got)
	}
	if got := norm(seq{token.NUMBER, token.PLUS, token.NUMBER}, 2); len(got) != 2 {
		t.Errorf("norm = %s, want 2 symbols", got)
	}
}

// TestConcatIsAbsorbing: A ⊕ ∅ = ∅ for anything still short of k. During the
// fixpoint an empty set means "not computed yet", so a body containing such a
// symbol must contribute nothing this round — contributing the prefix instead
// would let a wrong, too-short string into a FIRST set and stay there.
func TestConcatIsAbsorbing(t *testing.T) {
	a := newKset(seq{token.NUMBER})
	if got := concat(a, kset{}, 2); len(got) != 0 {
		t.Errorf("concat with the empty set = %v, want empty", got)
	}
	// A full string, by contrast, ignores whatever follows it.
	if got := concat(a, kset{}, 1); !got.has(seq{token.NUMBER}) {
		t.Errorf("concat = %v, want NUMBER to survive", got)
	}
}

// ------------------------------------------------------------------ table --

// TestTableBuildsForEveryK: the grammar has to be LL(k) at every k the parser
// offers, or NewK is a promise it cannot keep.
func TestTableBuildsForEveryK(t *testing.T) {
	for k := MinK; k <= MaxK; k++ {
		if _, err := newTable(loxGrammar(), k); err != nil {
			t.Errorf("k = %d: %v", k, err)
		}
	}
}

// TestTableDump keeps the debugging view honest — it is the one place the
// table is readable by a human, and docs/llk-parser.md quotes this row. A row
// is FOLLOW plus the rule's own operators: every lookahead that is not '-' or
// '+' has to send termTail to ε, or the ladder would not come back up.
func TestTableDump(t *testing.T) {
	tab, err := newTable(loxGrammar(), 1)
	if err != nil {
		t.Fatal(err)
	}
	dump := tab.String()

	for _, want := range []string{
		"LL(1) table",
		"termTail",
		"termTail → MINUS factor termTail",
		"termTail → PLUS factor termTail",
		"termTail → ε",
		"primary → LEFT_PAREN expression RIGHT_PAREN",
	} {
		if !strings.Contains(dump, want) {
			t.Errorf("table dump is missing %q", want)
		}
	}
	if strings.Contains(dump, "{action}") {
		t.Error("the dump shows actions; productions should read as grammar")
	}
}

// TestConflictIsReported is the check recursive descent cannot perform. This
// grammar is LL(2) and not LL(1): both productions start with a number, so one
// token of lookahead cannot choose, and two can.
func TestConflictIsReported(t *testing.T) {
	demo := func() *grammar {
		g := newGrammar()
		g.add("s", term(token.NUMBER, ""), term(token.PLUS, ""), term(token.NUMBER, ""))
		g.add("s", term(token.NUMBER, ""))
		g.entry("s", "Expect a number.")
		return g
	}

	_, err := newTable(demo(), 1)
	conflict, ok := err.(*ConflictError)
	if !ok {
		t.Fatalf("k = 1: got %v, want a *ConflictError", err)
	}
	if conflict.Head != "s" || !conflict.Lookahead.full(1) {
		t.Errorf("conflict reported as %v", conflict)
	}

	if _, err := newTable(demo(), 2); err != nil {
		t.Errorf("k = 2: %v, want the extra token of lookahead to resolve it", err)
	}
}

// TestGrammarValidation: a body naming a rule nobody wrote is a typo, and it
// has to fail loudly at build time rather than as an empty FIRST set that
// rejects every input.
func TestGrammarValidation(t *testing.T) {
	g := newGrammar()
	g.add("s", nt("nowhere"))
	g.entry("s", "Expect something.")

	if _, err := newTable(g, 1); err == nil {
		t.Error("built a table for a grammar with an undefined nonterminal")
	}
}

// TestGeneratedMessages covers the fallbacks: a grammar that names no message
// for a rule still has to produce a readable one, from the terminals the table
// would have accepted. The Lox grammar names all of its messages, so this drives
// a throwaway grammar — which is also the shortest demonstration that the driver
// is not tied to Lox at all.
func TestGeneratedMessages(t *testing.T) {
	defer errors.Reset()

	g := newGrammar()
	g.add("pair", term(token.LEFT_PAREN, ""), term(token.RIGHT_PAREN, ""), act(mkConst(nil)))
	g.entry("pair", "") // no message: force the fallbacks
	tab, err := newTable(g, 1)
	if err != nil {
		t.Fatal(err)
	}

	// No production predicts on a number: the message is built from the row.
	errors.Reset()
	p := &Parser{Tokens: withEOF(lexer.Lex("1")), tab: tab}
	_, err = p.run("pair")
	if pe, ok := err.(*errors.ParseError); !ok || pe.Message != "Expect one of '('." {
		t.Errorf("prediction miss gave %v, want \"Expect one of '('.\"", err)
	}

	// The ')' carries no message of its own, so it is named from its terminal.
	errors.Reset()
	p = &Parser{Tokens: withEOF(lexer.Lex("(")), tab: tab}
	_, err = p.run("pair")
	if pe, ok := err.(*errors.ParseError); !ok || pe.Message != "Expect ')'." {
		t.Errorf("terminal mismatch gave %v, want \"Expect ')'.\"", err)
	}
}

// TestErrorsReadWell: these two are printed at people who are editing a
// grammar, and a message that does not name the productions is useless.
func TestErrorsReadWell(t *testing.T) {
	g := newGrammar()
	g.add("s", term(token.NUMBER, ""), term(token.PLUS, ""))
	g.add("s", term(token.NUMBER, ""))
	g.entry("s", "Expect a number.")

	_, err := newTable(g, 1)
	msg := err.Error()
	for _, want := range []string{"not LL(1)", "NUMBER", "s → NUMBER PLUS", "s → NUMBER"} {
		if !strings.Contains(msg, want) {
			t.Errorf("conflict message %q is missing %q", msg, want)
		}
	}

	bad := newGrammar()
	bad.add("s", nt("nowhere"))
	bad.entry("s", "Expect something.")
	_, err = newTable(bad, 1)
	if msg := err.Error(); !strings.Contains(msg, "nowhere") {
		t.Errorf("grammar error %q does not name the missing rule", msg)
	}
}

func TestKReportsTheLookahead(t *testing.T) {
	for k := MinK; k <= MaxK; k++ {
		if got := parserFor(t, "1;", k).K(); got != k {
			t.Errorf("K() = %d, want %d", got, k)
		}
	}
	if got := New(lexer.Lex("1;")).K(); got != DefaultK {
		t.Errorf("New gave k = %d, want the default %d", got, DefaultK)
	}
}

func TestNewKRejectsOutOfRangeK(t *testing.T) {
	for _, k := range []int{0, -1, MaxK + 1} {
		if _, err := NewK(lexer.Lex("1;"), k); err == nil {
			t.Errorf("k = %d: accepted", k)
		}
	}
}

// ------------------------------------------------------------------ parse --

// TestParse is deliberately the same table of cases as the recursive-descent
// parser's. Two different algorithms, one set of expected trees.
func TestParse(t *testing.T) {
	cases := map[string]string{
		"1 + 2 * 3":       "(+ 1 (* 2 3))",
		"1 - 2 * 3":       "(- 1 (* 2 3))",
		"1 - 2 - 3":       "(- (- 1 2) 3)",
		"1 < 2 == true":   "(== (< 1 2) true)",
		"2 >= 1 != false": "(!= (>= 2 1) false)",
		"-123 * (45.67)":  "(* (- 123) (group 45.67))",
		"!!nil":           "(! (! nil))",
		`"a" + "b"`:       "(+ a b)",
	}

	for src, want := range cases {
		got, err := parse(t, src, DefaultK)
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

// TestLeftAssociativity pins the action-placement trick in level(). Moving
// mkBinary after the recursive tail still parses every input here — it just
// nests them the other way, and only this test would notice.
func TestLeftAssociativity(t *testing.T) {
	cases := map[string]string{
		"1 - 2 - 3 - 4": "(- (- (- 1 2) 3) 4)",
		"1 / 2 / 3":     "(/ (/ 1 2) 3)",
		"1 + 2 - 3 + 4": "(+ (- (+ 1 2) 3) 4)",
	}
	for src, want := range cases {
		got, err := parse(t, src, DefaultK)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if got != want {
			t.Errorf("%q:\n  got  %s\n  want %s", src, got, want)
		}
	}
	errors.Reset()
}

// TestEveryKAgrees: k is how far the parser may look, not what it parses. The
// tree must not depend on it.
func TestEveryKAgrees(t *testing.T) {
	defer errors.Reset()

	srcs := []string{
		"1 + 2 * 3", "(1 + 2) * 3", "-1 - -2", "!true == false",
		"1 < 2 <= 3", `"x" != nil`, "((((1))))",
	}
	for _, src := range srcs {
		want, err := parse(t, src, MinK)
		if err != nil {
			t.Fatalf("%q at k = %d: %v", src, MinK, err)
		}
		for k := MinK + 1; k <= MaxK; k++ {
			got, err := parse(t, src, k)
			if err != nil {
				t.Errorf("%q at k = %d: %v", src, k, err)
				continue
			}
			if got != want {
				t.Errorf("%q: k = %d gives %s, k = %d gives %s", src, k, got, MinK, want)
			}
		}
	}
}

// TestParseErrors covers each shape of failure, at every k: the answer to "is
// this a syntax error" must not depend on the lookahead either.
func TestParseErrors(t *testing.T) {
	defer errors.Reset()

	for _, src := range []string{"(1 + 2", "1 +", "1 + 2)", "", "*", "1 2", ")", "(", "1 +* 2"} {
		for k := MinK; k <= MaxK; k++ {
			_, err := parse(t, src, k)
			if err == nil {
				t.Errorf("%q at k = %d: parsed without error", src, k)
				continue
			}
			if _, ok := err.(*errors.ParseError); !ok {
				t.Errorf("%q at k = %d: error is %T, want *errors.ParseError", src, k, err)
			}
			if !errors.HadError {
				t.Errorf("%q at k = %d: error returned but HadError not set", src, k)
			}
		}
	}
}

// TestErrorMessages: a table-driven parser has no context to write a message
// from, so the messages come from the grammar. These are the ones a user sees.
func TestErrorMessages(t *testing.T) {
	defer errors.Reset()

	cases := map[string]string{
		"(1 + 2": "Expect ')' after expression.",
		"1 +":    "Expect expression.",
		"*":      "Expect expression.",
		"1 2":    "Expect end of expression.",
	}
	for src, want := range cases {
		_, err := parse(t, src, DefaultK)
		pe, ok := err.(*errors.ParseError)
		if !ok {
			t.Errorf("%q: got %v", src, err)
			continue
		}
		if pe.Message != want {
			t.Errorf("%q: message %q, want %q", src, pe.Message, want)
		}
	}
}

// --------------------------------------------------------------- parseAll --

func TestParseAll(t *testing.T) {
	defer errors.Reset()

	for k := MinK; k <= MaxK; k++ {
		exprs, errs := parserFor(t, "1 + 2; 3 * 4;", k).ParseAll()
		if len(errs) != 0 {
			t.Fatalf("k = %d: unexpected errors: %v", k, errs)
		}
		got := render(exprs)
		want := []string{"(+ 1 2)", "(* 3 4)"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("k = %d: got %v, want %v", k, got, want)
		}
	}
}

func TestParseAllRecovers(t *testing.T) {
	defer errors.Reset()

	exprs, errs := parserFor(t, "1 + ; 2 * ; 3 + 4;", DefaultK).ParseAll()

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

func TestParseAllMissingSemicolon(t *testing.T) {
	defer errors.Reset()

	exprs, errs := parserFor(t, "1 + 2", DefaultK).ParseAll()

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}
	if pe, ok := errs[0].(*errors.ParseError); ok && pe.Message != "Expect ';' after expression." {
		t.Errorf("message %q", pe.Message)
	}
	if len(exprs) != 0 {
		t.Errorf("got %v, want no expressions", render(exprs))
	}
}

// TestParseAllTerminates: every iteration must consume a token — on the error
// path that is synchronize's job. A regression shows up as this package timing
// out rather than as a failure, so the inputs are the nastiest ones available.
func TestParseAllTerminates(t *testing.T) {
	defer errors.Reset()

	for _, src := range []string{")))", ";;;", "* * *", "1 2 3", "+;+;+;", "", ";", "((("} {
		for k := MinK; k <= MaxK; k++ {
			exprs, errs := parserFor(t, src, k).ParseAll()
			if src != "" && len(exprs) == 0 && len(errs) == 0 {
				t.Errorf("%q at k = %d: parsed to nothing at all, expected an error", src, k)
			}
		}
	}
}

// TestSynchronizeClearsTheStacks: recovery drops a half-expanded parse. If the
// work stack survived, the next statement would resume inside the failed one.
func TestSynchronizeClearsTheStacks(t *testing.T) {
	defer errors.Reset()

	p := parserFor(t, "(1 + ; 2 + 3;", DefaultK)
	if _, err := p.run(nExprStatement); err == nil {
		t.Fatal("expected an error")
	}
	if len(p.work) == 0 {
		t.Fatal("nothing was left half-expanded; the test no longer tests anything")
	}

	p.synchronize()

	if len(p.work) != 0 || len(p.vals.vals) != 0 {
		t.Errorf("after synchronize: %d symbols and %d values left", len(p.work), len(p.vals.vals))
	}
	exprs, errs := p.ParseAll()
	if len(errs) != 0 || len(exprs) != 1 || render(exprs)[0] != "(+ 2 3)" {
		t.Errorf("resumed as %v (errors %v), want [(+ 2 3)]", render(exprs), errs)
	}
}

// TestNewAppendsEOF: the lookahead window and the FOLLOW sets both spell "end
// of input" as an EOF token, so a stream without one has to be given one.
func TestNewAppendsEOF(t *testing.T) {
	defer errors.Reset()

	p := New([]token.Token{{Type: token.NUMBER, Lexeme: "1", Literal: 1.0, Line: 1}})
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := (&ast.Printer{}).Print(expr); got != "1" {
		t.Errorf("got %s, want 1", got)
	}
}

// ------------------------------------------------------------ parseProgram --

func renderProgram(statements []ast.Stmt) []string {
	printer := ast.NewStmtPrinter(&ast.Printer{})
	out := make([]string, len(statements))
	for i, statement := range statements {
		out[i] = printer.Print(statement)
	}
	return out
}

func TestParseProgram(t *testing.T) {
	defer errors.Reset()

	const source = `var a = 1; print a; a = a + 1; { var a; print a; }`
	want := []string{
		"(var a 1)",
		"(print a)",
		"(expr (= a (+ a 1)))",
		"(block (var a) (print a))",
	}

	for k := MinK; k <= MaxK; k++ {
		statements, errs := parserFor(t, source, k).ParseProgram()
		if len(errs) != 0 {
			t.Fatalf("k=%d: unexpected errors: %v", k, errs)
		}
		got := renderProgram(statements)
		if len(got) != len(want) {
			t.Fatalf("k=%d: got %v, want %v", k, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("k=%d statement %d = %s, want %s", k, i, got[i], want[i])
			}
		}
	}
}

func TestWholeProgramGrammarBuildsStatementList(t *testing.T) {
	defer errors.Reset()

	p := parserFor(t, `var a = 1; { print a; }`, DefaultK)
	value, err := p.run(nProgram)
	if err != nil {
		t.Fatalf("run(program): %v", err)
	}
	statements, ok := value.([]ast.Stmt)
	if !ok {
		t.Fatalf("program produced %T, want []ast.Stmt", value)
	}
	if got, want := renderProgram(statements), []string{"(var a 1)", "(block (print a))"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("program = %v, want %v", got, want)
	}
}

func TestParseProgramInvalidAssignmentTarget(t *testing.T) {
	defer errors.Reset()

	statements, errs := parserFor(t, "a + b = 3; print 4;", DefaultK).ParseProgram()
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}
	pe, ok := errs[0].(*errors.ParseError)
	if !ok || pe.Token.Type != token.EQUAL || pe.Message != "Invalid assignment target." {
		t.Errorf("error = %v, want invalid target at '='", errs[0])
	}
	if got := renderProgram(statements); len(got) != 1 || got[0] != "(print 4)" {
		t.Errorf("recovered as %v, want [(print 4)]", got)
	}
}
