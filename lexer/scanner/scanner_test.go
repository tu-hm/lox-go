// Package scanner_test exercises the scanner through its exported API only,
// the same way the parser will use it in chapter 6.
//
// Note: these tests must not call t.Parallel(). The scanner reports through
// the package-level errors.HadError flag and writes to os.Stderr, both of
// which are process-global state that the helpers below swap out.
package scanner_test

import (
	"bytes"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"compiler101/lexer/scanner"
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// ---------------------------------------------------------------- helpers --

// want is the expected shape of one token.
type want struct {
	typ     token.TokenType
	lexeme  string
	literal any
	line    int
}

func eof(line int) want {
	return want{token.EOF, "", nil, line}
}

// scan runs the scanner with a clean error state.
func scan(t *testing.T, src string) []token.Token {
	t.Helper()
	errors.Reset()
	return scanner.NewScanner(src).ScanToken()
}

// assertTokens checks every field of every token, including the trailing EOF.
func assertTokens(t *testing.T, src string, wants ...want) {
	t.Helper()

	got := scan(t, src)
	if len(got) != len(wants) {
		t.Fatalf("scanning %q: got %d tokens, want %d\ngot: %v", src, len(got), len(wants), got)
	}

	for i, w := range wants {
		g := got[i]
		if g.Type != w.typ || g.Lexeme != w.lexeme || !reflect.DeepEqual(g.Literal, w.literal) || g.Line != w.line {
			t.Errorf("scanning %q: token %d =\n  got  {%s lexeme=%q literal=%#v line=%d}\n  want {%s lexeme=%q literal=%#v line=%d}",
				src, i, g.Type, g.Lexeme, g.Literal, g.Line, w.typ, w.lexeme, w.literal, w.line)
		}
	}
}

// typesOf projects a token slice down to its type sequence.
func typesOf(toks []token.Token) []token.TokenType {
	types := make([]token.TokenType, len(toks))
	for i, tk := range toks {
		types[i] = tk.Type
	}
	return types
}

// assertTypes checks only the token type sequence, for cases where lexemes and
// literals are not the point.
func assertTypes(t *testing.T, src string, wants ...token.TokenType) {
	t.Helper()

	got := typesOf(scan(t, src))
	if !reflect.DeepEqual(got, wants) {
		t.Errorf("scanning %q:\n  got  %v\n  want %v", src, got, wants)
	}
}

// captureStderr swaps os.Stderr for a pipe while fn runs and returns whatever
// was written. errors.report resolves os.Stderr at call time, so this works.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	defer func() {
		os.Stderr = old
	}()

	fn()

	_ = w.Close()
	return <-done
}

// ------------------------------------------------------------ basic shape --

func TestScanTokensAlwaysEndsWithEOF(t *testing.T) {
	tests := []struct {
		name string
		src  string
		line int
	}{
		{"empty source", "", 1},
		{"only whitespace", "   \t\r  ", 1},
		{"only a comment", "// nothing here", 1},
		{"trailing newline", "var\n", 2},
		{"several newlines", "\n\n\n", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scan(t, tt.src)

			if len(got) == 0 {
				t.Fatalf("scanning %q: got no tokens, want at least EOF", tt.src)
			}

			last := got[len(got)-1]
			if last.Type != token.EOF {
				t.Errorf("scanning %q: last token = %s, want EOF", tt.src, last.Type)
			}
			if last.Lexeme != "" {
				t.Errorf("scanning %q: EOF lexeme = %q, want empty", tt.src, last.Lexeme)
			}
			if last.Line != tt.line {
				t.Errorf("scanning %q: EOF line = %d, want %d", tt.src, last.Line, tt.line)
			}
		})
	}
}

func TestScanTokensLineNumbersStartAtOne(t *testing.T) {
	got := scan(t, "var")

	if got[0].Line != 1 {
		t.Errorf("first token line = %d, want 1", got[0].Line)
	}
}

// ---------------------------------------------------------------- lexemes --

func TestScanTokensSingleCharacter(t *testing.T) {
	assertTypes(t, "(){},.-+;*/",
		token.LEFT_PAREN, token.RIGHT_PAREN,
		token.LEFT_BRACE, token.RIGHT_BRACE,
		token.COMMA, token.DOT, token.MINUS, token.PLUS,
		token.SEMICOLON, token.STAR, token.SLASH,
		token.EOF,
	)
}

func TestScanTokensOneOrTwoCharacterOperators(t *testing.T) {
	tests := []struct {
		src  string
		want token.TokenType
	}{
		{"!", token.BANG},
		{"!=", token.BANG_EQUAL},
		{"=", token.EQUAL},
		{"==", token.EQUAL_EQUAL},
		{"<", token.LESS},
		{"<=", token.LESS_EQUAL},
		{">", token.GREATER},
		{">=", token.GREATER_EQUAL},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			assertTokens(t, tt.src,
				want{tt.want, tt.src, nil, 1},
				eof(1),
			)
		})
	}
}

// The maximal-munch rule: "==" is one token, not two.
func TestScanTokensPrefersLongestOperator(t *testing.T) {
	assertTypes(t, "===",
		token.EQUAL_EQUAL, token.EQUAL,
		token.EOF,
	)

	assertTypes(t, "!!=",
		token.BANG, token.BANG_EQUAL,
		token.EOF,
	)
}

// -------------------------------------------------------------- comments ---

func TestScanTokensComments(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []token.TokenType
	}{
		{
			"comment consumes rest of line",
			"1 // + 2\n3",
			[]token.TokenType{token.NUMBER, token.NUMBER, token.EOF},
		},
		{
			"comment at end of input without newline",
			"1 // trailing",
			[]token.TokenType{token.NUMBER, token.EOF},
		},
		{
			"empty comment",
			"//",
			[]token.TokenType{token.EOF},
		},
		{
			"lone slash is division",
			"1/2",
			[]token.TokenType{token.NUMBER, token.SLASH, token.NUMBER, token.EOF},
		},
		{
			"comment does not swallow the newline",
			"// a\n// b\nx",
			[]token.TokenType{token.IDENTIFIER, token.EOF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTypes(t, tt.src, tt.want...)
		})
	}
}

func TestScanTokensCommentsStillCountLines(t *testing.T) {
	// The comment body is skipped but the newline after it must not be.
	assertTokens(t, "// one\n// two\nx",
		want{token.IDENTIFIER, "x", nil, 3},
		eof(3),
	)
}

// --------------------------------------------------------------- strings ---

func TestScanTokensStrings(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		lexeme  string
		literal string
	}{
		{"simple", `"hello"`, `"hello"`, "hello"},
		{"empty", `""`, `""`, ""},
		{"with spaces", `"hello world"`, `"hello world"`, "hello world"},
		{"looks like code", `"var x = 1;"`, `"var x = 1;"`, "var x = 1;"},
		{"looks like a comment", `"// not a comment"`, `"// not a comment"`, "// not a comment"},
		{"digits", `"123"`, `"123"`, "123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The lexeme keeps the quotes; the literal drops them.
			assertTokens(t, tt.src,
				want{token.STRING, tt.lexeme, tt.literal, 1},
				eof(1),
			)
		})
	}
}

func TestScanTokensMultiLineString(t *testing.T) {
	src := "\"a\nb\"\nx"

	// A string may span lines. The STRING token is stamped with the line it
	// *ends* on, matching the book.
	assertTokens(t, src,
		want{token.STRING, "\"a\nb\"", "a\nb", 2},
		want{token.IDENTIFIER, "x", nil, 3},
		eof(3),
	)
}

func TestScanTokensUnterminatedString(t *testing.T) {
	var got []token.Token
	stderr := captureStderr(t, func() {
		errors.Reset()
		got = scanner.NewScanner(`"no closing quote`).ScanToken()
	})

	if !errors.HadError {
		t.Error("HadError = false, want true for an unterminated string")
	}
	if !strings.Contains(stderr, "Unterminated string.") {
		t.Errorf("stderr = %q, want it to mention \"Unterminated string.\"", stderr)
	}

	// No STRING token is produced — only EOF.
	if types := typesOf(got); !reflect.DeepEqual(types, []token.TokenType{token.EOF}) {
		t.Errorf("tokens = %v, want just [EOF]", types)
	}

	errors.Reset()
}

// --------------------------------------------------------------- numbers ---

func TestScanTokensNumbers(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		literal float64
	}{
		{"integer", "123", 123},
		{"zero", "0", 0},
		{"decimal", "123.456", 123.456},
		{"leading zero decimal", "0.5", 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTokens(t, tt.src,
				want{token.NUMBER, tt.src, tt.literal, 1},
				eof(1),
			)
		})
	}
}

// Lox does not allow a leading or trailing decimal point, so the dot scans as
// its own DOT token. This matters later: DOT is property access.
func TestScanTokensNumbersWithDanglingDots(t *testing.T) {
	t.Run("leading dot", func(t *testing.T) {
		assertTokens(t, ".5",
			want{token.DOT, ".", nil, 1},
			want{token.NUMBER, "5", 5.0, 1},
			eof(1),
		)
	})

	t.Run("trailing dot", func(t *testing.T) {
		assertTokens(t, "123.",
			want{token.NUMBER, "123", 123.0, 1},
			want{token.DOT, ".", nil, 1},
			eof(1),
		)
	})

	t.Run("method call on a number", func(t *testing.T) {
		assertTypes(t, "123.sqrt",
			token.NUMBER, token.DOT, token.IDENTIFIER,
			token.EOF,
		)
	})
}

func TestScanTokensNumberLiteralIsFloat64(t *testing.T) {
	got := scan(t, "42")

	// Lox has one number type. The interpreter will type-assert float64, so a
	// stray int here would panic at runtime.
	if _, ok := got[0].Literal.(float64); !ok {
		t.Errorf("literal has type %T, want float64", got[0].Literal)
	}
}

// --------------------------------------------- identifiers and keywords ----

func TestScanTokensKeywords(t *testing.T) {
	keywords := []struct {
		src string
		typ token.TokenType
	}{
		{"and", token.AND},
		{"class", token.CLASS},
		{"else", token.ELSE},
		{"false", token.FALSE},
		{"for", token.FOR},
		{"fun", token.FUN},
		{"if", token.IF},
		{"nil", token.NIL},
		{"or", token.OR},
		{"print", token.PRINT},
		{"return", token.RETURN},
		{"super", token.SUPER},
		{"this", token.THIS},
		{"true", token.TRUE},
		{"var", token.VAR},
		{"while", token.WHILE},
	}

	for _, kw := range keywords {
		t.Run(kw.src, func(t *testing.T) {
			assertTokens(t, kw.src,
				want{kw.typ, kw.src, nil, 1},
				eof(1),
			)
		})
	}
}

func TestScanTokensIdentifiers(t *testing.T) {
	identifiers := []string{
		"x",
		"foo",
		"fooBar",
		"foo_bar",
		"_private",
		"a1",
		"x123y",
		// Maximal munch again: a keyword is only a keyword when it is the
		// whole identifier.
		"orchid",
		"variable",
		"iffy",
		"printer",
		"class_",
	}

	for _, id := range identifiers {
		t.Run(id, func(t *testing.T) {
			assertTokens(t, id,
				want{token.IDENTIFIER, id, nil, 1},
				eof(1),
			)
		})
	}
}

// -------------------------------------------------------------- literals ---

func TestScanTokensNonLiteralTokensHaveNilLiteral(t *testing.T) {
	// Only NUMBER and STRING carry a literal value. Everything else must be
	// nil, or the interpreter cannot tell "no value" from a value.
	got := scan(t, `var x = 1 + "two"; // done`)

	for _, tk := range got {
		switch tk.Type {
		case token.NUMBER, token.STRING:
			if tk.Literal == nil {
				t.Errorf("%s %q: literal is nil, want a value", tk.Type, tk.Lexeme)
			}
		default:
			if tk.Literal != nil {
				t.Errorf("%s %q: literal = %#v, want nil", tk.Type, tk.Lexeme, tk.Literal)
			}
		}
	}
}

// ----------------------------------------------------------- line counts ---

func TestScanTokensLineNumbers(t *testing.T) {
	src := "1\n2\n\n3"

	assertTokens(t, src,
		want{token.NUMBER, "1", 1.0, 1},
		want{token.NUMBER, "2", 2.0, 2},
		want{token.NUMBER, "3", 3.0, 4},
		eof(4),
	)
}

func TestScanTokensCarriageReturnsAreWhitespace(t *testing.T) {
	// CRLF: the \r is skipped, the \n bumps the line.
	assertTokens(t, "1\r\n2",
		want{token.NUMBER, "1", 1.0, 1},
		want{token.NUMBER, "2", 2.0, 2},
		eof(2),
	)
}

// ----------------------------------------------------------------- errors --

func TestScanTokensUnexpectedCharacter(t *testing.T) {
	var got []token.Token
	stderr := captureStderr(t, func() {
		errors.Reset()
		got = scanner.NewScanner("@").ScanToken()
	})

	if !errors.HadError {
		t.Error("HadError = false, want true for an unexpected character")
	}
	if !strings.Contains(stderr, "Unexpected character.") {
		t.Errorf("stderr = %q, want it to mention \"Unexpected character.\"", stderr)
	}
	if !strings.Contains(stderr, "[line 1]") {
		t.Errorf("stderr = %q, want it to report line 1", stderr)
	}
	if len(got) != 1 || got[0].Type != token.EOF {
		t.Errorf("tokens = %v, want just [EOF]", got)
	}

	errors.Reset()
}

func TestScanTokensReportsErrorOnTheRightLine(t *testing.T) {
	stderr := captureStderr(t, func() {
		errors.Reset()
		scanner.NewScanner("1\n2\n@").ScanToken()
	})

	if !strings.Contains(stderr, "[line 3]") {
		t.Errorf("stderr = %q, want the error reported on line 3", stderr)
	}

	errors.Reset()
}

func TestScanTokensKeepsGoingAfterAnError(t *testing.T) {
	var got []token.Token
	captureStderr(t, func() {
		errors.Reset()
		got = scanner.NewScanner("1 @ 2").ScanToken()
	})

	// The scanner reports and continues, so one bad character does not hide
	// the rest of the errors in the file.
	wantTypes := []token.TokenType{token.NUMBER, token.NUMBER, token.EOF}
	if types := typesOf(got); !reflect.DeepEqual(types, wantTypes) {
		t.Errorf("tokens = %v, want %v", types, wantTypes)
	}

	errors.Reset()
}

func TestScanTokensCleanInputSetsNoError(t *testing.T) {
	errors.Reset()
	scanner.NewScanner(`var x = 1 + 2 * (3 - 4) / 5; print "ok";`).ScanToken()

	if errors.HadError {
		t.Error("HadError = true, want false for valid source")
	}

	errors.Reset()
}

// ------------------------------------------------------------ integration --

func TestScanTokensProgram(t *testing.T) {
	src := `// Compute a factorial.
fun fact(n) {
  if (n <= 1) return 1;
  return n * fact(n - 1);
}

print fact(5); // 120
`

	assertTypes(t, src,
		token.FUN, token.IDENTIFIER, token.LEFT_PAREN, token.IDENTIFIER, token.RIGHT_PAREN, token.LEFT_BRACE,
		token.IF, token.LEFT_PAREN, token.IDENTIFIER, token.LESS_EQUAL, token.NUMBER, token.RIGHT_PAREN,
		token.RETURN, token.NUMBER, token.SEMICOLON,
		token.RETURN, token.IDENTIFIER, token.STAR, token.IDENTIFIER,
		token.LEFT_PAREN, token.IDENTIFIER, token.MINUS, token.NUMBER, token.RIGHT_PAREN, token.SEMICOLON,
		token.RIGHT_BRACE,
		token.PRINT, token.IDENTIFIER, token.LEFT_PAREN, token.NUMBER, token.RIGHT_PAREN, token.SEMICOLON,
		token.EOF,
	)
}

func TestScanTokensProgramLineNumbers(t *testing.T) {
	src := "// header\nvar a = 1;\n\nvar b = 2;\n"

	got := scan(t, src)

	lines := make([]int, len(got))
	for i, tk := range got {
		lines[i] = tk.Line
	}

	// var a = 1 ;  -> line 2       var b = 2 ;  -> line 4       EOF -> line 5
	wantLines := []int{2, 2, 2, 2, 2, 4, 4, 4, 4, 4, 5}
	if !reflect.DeepEqual(lines, wantLines) {
		t.Errorf("lines = %v, want %v\ntokens: %v", lines, wantLines, got)
	}
}

// ----------------------------------------------------------------- token ---

func TestTokenStringUsesValueReceiver(t *testing.T) {
	tk := token.Token{Type: token.NUMBER, Lexeme: "123", Literal: 123.0, Line: 1}

	got := tk.String()
	if want := "NUMBER 123 123"; got != want {
		t.Errorf("Token.String() = %q, want %q", got, want)
	}
}
