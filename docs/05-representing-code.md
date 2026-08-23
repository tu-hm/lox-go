# Chapter 5 — Representing Code, in Go

Plan for porting [craftinginterpreters.com/representing-code.html](https://craftinginterpreters.com/representing-code.html)
to this repo (`module compiler101`, Go 1.26).

**Goal of the chapter:** you finish with a data structure that can hold a parsed
expression, and one operation over it (a pretty-printer). No parser yet — that's
chapter 6. You will hand-build a tree in a test to prove the shape works.

> Every Go snippet below was compiled and run against a copy of this repo before
> being written down — the generator produces the file shown in §6, and the
> printer test in §8 passes.
>
> **Status: chapter 5 complete.** M0–M5 and all three challenges are done.
> `go test ./...` green; `gofmt -l .` and `go vet ./...` clean; `go generate
> ./...` is a no-op against the committed tree. Coverage: 100% of `ast/`
> (excluding the four unreachable `isExpr` markers), 98.9% of the scanner.
> New this chapter: `ast/{expr.go,printer.go,rpn.go,value.go,gen.go}`,
> `cmd/genast/`, and `docs/grammar.md`. Next: chapter 6, parsing expressions.

---

## Contents

1. [Where the repo is now](#1-where-the-repo-is-now)
2. [Chapter 4 cleanup — done](#2-chapter-4-cleanup--done)
3. [The grammar](#3-the-grammar-51)
4. [Design decision: how to represent the AST in Go](#4-design-decision-how-to-represent-the-ast-in-go)
5. [Package layout](#5-package-layout)
6. [The AST generator](#6-the-ast-generator-54)
7. [The printer](#7-the-printer-55)
8. [Tests](#8-tests)
9. [Milestones](#9-milestones)
10. [Go gotchas](#10-go-gotchas-specific-to-this-chapter)
11. [Chapter challenges](#11-chapter-challenges)
12. [What chapter 6 will need](#12-what-chapter-6-will-need-from-you)

---

## 1. Where the repo is now

```
compiler101/
├── main.go                       # CLI: Run / RunFile / RunPrompt          ✅
├── lexer/
│   ├── lexer.go                  # Lex(source) []token.Token — front door  ✅
│   ├── token/token.go            # TokenType consts, Token struct          ✅
│   └── scanner/
│       ├── scanner.go            # ScannerImpl — chapter 4 complete        ✅
│       └── scanner_test.go       # 25 tests / 63 subtests, 98.9% coverage  ✅
├── parser/                       # empty — chapter 6 lands here
├── pkg/errors/errors.go          # HadError + Error + Reset                ✅
└── utils/utils.go                # IsDigit / IsAlpha / IsAlphaNumeric      ✅
```

**Chapter 4 is done and tested.** `go test ./...` is green, and
`go run . script.lox` prints a token stream ending in `EOF`. Chapter 5 adds a
new `ast/` package and a code generator; it does not touch the scanner.

---

## 2. Chapter 4 cleanup — done

All ten issues are fixed and pinned by tests. Recorded here so the reasoning
survives; skip to §3 if you just want the chapter 5 plan.

### What changed

| # | File | Was | Now |
|---|------|-----|-----|
| 1 | `scanner.go` | No `EOF` token appended | `ScanToken()` always appends `EOF`. The ch. 6 parser peeks one past the last token and would have panicked without it. |
| 2 | `scanner.go` | No `case '"'` — quotes errored and the contents fell through to `identifier()`, so `"two"` became `IDENTIFIER two` | `stringLiteral()` scans to the closing quote, bumps `line` on embedded newlines, reports `Unterminated string.`, and stores the unquoted text as the literal. |
| 3 | `scanner.go` | No `case '/'` — division errored and comment bodies were tokenised as code | `//` consumes to end of line (leaving the `\n` so the line counter still fires); a lone `/` is `SLASH`. |
| 4 | `main.go` | `len(args) == 1` (meaning *no* args) called `RunFile(args[1])` → panic | Strips `os.Args[0]` first, then `>1` → usage + exit 64, `==1` → `RunFile`, else REPL. |
| 5 | `main.go` | `Run()` was empty | Calls `lexer.Lex` and prints the token stream. |
| 6 | `scanner.go` | `line` started at `0` | `NewScanner` sets `line: 1`. |
| 7 | `scanner.go` | `addToken` stored the lexeme as `Literal` for every token (`+` carried `Literal: "+"`) | `addToken` delegates to `addTokenWithValue(t, nil)`. Only NUMBER and STRING carry literals. |
| 8 | `token.go` | `String()` had a pointer receiver, so a `Token` value didn't satisfy `fmt.Stringer` — `%v` printed `{NUMBER 123 123 1}` | Value receiver; both `Token` and `*Token` satisfy it. |
| 9 | `token.go` | `%s` on `Literal any` rendered a float as `%!s(float64=123)` | Uses `%v`. |
| 10 | `lexer.go` | Empty `Lex()` nothing called | One-line front door: `Lex(source) []token.Token`. |

Two extras worth knowing about, since they weren't on the original list:

- **`RunPrompt` looped forever on Ctrl-D.** `bufio.ReadString` returns `io.EOF`
  with no error check, so the REPL spun printing `> `. It now returns on any
  read error.
- **`errors.Reset()` was added.** The REPL needs it between lines, and the tests
  need it because `HadError` is package-global. Documented on the function:
  tests asserting on it cannot use `t.Parallel()`.

### Same input, before and after

`var x = 1 + "two"; // done`

```
BEFORE                                          AFTER
[line 0] Error: Unexpected character.  ×4       (no errors)

VAR         literal=var    line=0               VAR         literal=<nil>   line=1
IDENTIFIER  literal=x      line=0               IDENTIFIER  literal=<nil>   line=1
EQUAL       literal==      line=0               EQUAL       literal=<nil>   line=1
NUMBER      literal=1      line=0               NUMBER      literal=1       line=1
PLUS        literal=+      line=0               PLUS        literal=<nil>   line=1
IDENTIFIER  literal=two    line=0   ← wrong     STRING      literal=two     line=1
SEMICOLON   literal=;      line=0               SEMICOLON   literal=<nil>   line=1
IDENTIFIER  literal=done   line=0   ← leaked    EOF         literal=<nil>   line=1
(no EOF)                            ← fatal
```

### The test suite

`lexer/scanner/scanner_test.go` — 25 tests, 63 subtests, **98.9% statement
coverage** of the scanner. It is `package scanner_test`, so it exercises only
the exported API, the same way the ch. 6 parser will.

Three helpers carry most of it:

- `assertTokens(t, src, wants...)` — checks type, lexeme, literal *and* line for
  every token including the trailing EOF.
- `assertTypes(t, src, wants...)` — type sequence only, for shape tests.
- `captureStderr(t, fn)` — swaps `os.Stderr` for a pipe so the error tests can
  assert the actual message rather than just the `HadError` flag. Works because
  `errors.report` resolves `os.Stderr` at call time.

What's covered, grouped:

| Group | Pins down |
|---|---|
| EOF & line base | EOF always present with the right line, even for empty/whitespace/comment-only input; lines start at 1 |
| Single-char tokens | all eleven, including `/` |
| Operators | `!` `!=` `=` `==` `<` `<=` `>` `>=`, plus maximal munch: `===` → `EQUAL_EQUAL EQUAL` |
| Comments | body skipped, newline preserved, comment at EOF, empty `//`, lone `/` as division |
| Strings | simple, empty, spaces, contents that look like code or a comment; lexeme keeps quotes, literal drops them |
| Multi-line strings | line counter advances; token is stamped with the line it **ends** on |
| Numbers | ints, decimals; literal is always `float64` (the interpreter will type-assert it) |
| Dangling dots | `.5` → `DOT NUMBER`, `123.` → `NUMBER DOT`, `123.sqrt` → `NUMBER DOT IDENTIFIER` |
| Keywords | all 16 |
| Identifiers | `_private`, `a1`, and near-misses like `orchid`, `iffy`, `class_` that must not become keywords |
| Literal discipline | only NUMBER and STRING have non-nil `Literal` |
| Line counting | blank lines, CRLF, whole-program line map |
| Errors | unexpected char, unterminated string, correct line, scanning **continues** after an error, clean input sets no error |
| `Token.String()` | value receiver + `%v`, regression test for #8 and #9 |

Run it:

```sh
go test ./lexer/... -v
go test ./lexer/... -cover
```

---

## 3. The grammar (5.1)

The chapter's payload is the notation itself. Write the grammar down in a file —
you will edit it in every chapter from here to the end of the book, and having
one canonical copy beats re-deriving it from the parser code.

**Create `docs/grammar.md`:**

```
expression     → literal
               | unary
               | binary
               | grouping ;

literal        → NUMBER | STRING | "true" | "false" | "nil" ;
grouping       → "(" expression ")" ;
unary          → ( "-" | "!" ) expression ;
binary         → expression operator expression ;
operator       → "==" | "!=" | "<" | "<=" | ">" | ">="
               | "+"  | "-"  | "*"  | "/" ;
```

Notation, so the file is readable later:

- `→` separates a rule head from its body; `;` ends the rule.
- **Terminals** are quoted (`"("`) or CAPITALISED (`NUMBER`) — these are your
  `token.TokenType` values.
- **Nonterminals** are lowercase — these become AST node types.
- `|` alternation, `( )` grouping, `*` zero-or-more, `+` one-or-more, `?` optional.

This grammar is deliberately **ambiguous** — `1 + 2 * 3` has two valid parses.
Chapter 6 rewrites it into a precedence-stratified version. Don't try to fix it now.

The mapping to code is mechanical, and it's the whole point of the chapter:

| Grammar rule | AST node | Fields |
|---|---|---|
| `binary` | `Binary` | `Left Expr`, `Operator token.Token`, `Right Expr` |
| `grouping` | `Grouping` | `Expression Expr` |
| `literal` | `Literal` | `Value any` |
| `unary` | `Unary` | `Operator token.Token`, `Right Expr` |

`operator` gets no node — the token itself carries which operator it is. That's
the "abstract" in abstract syntax tree: only what the interpreter needs survives.

---

## 4. Design decision: how to represent the AST in Go

This is the one real decision in the chapter, and Go forces you off the book's path.

### Why you can't copy the book directly

The book's `Expr` uses a **generic method**:

```java
abstract <R> R accept(Visitor<R> visitor);   // Java
```

Go has generics, but **methods cannot have type parameters**:

```go
func (e *Binary) Accept[R any](v Visitor[R]) R { ... }
// compile error: method must have no type parameters
```

So the generic-visitor translation is off the table. Three options remain.

---

### Option A — Visitor interface returning `any` ✅ recommended

```go
package ast

type Expr interface {
	Accept(v ExprVisitor) any
	isExpr() // unexported: seals the interface to this package
}

type ExprVisitor interface {
	VisitBinaryExpr(e *Binary) any
	VisitGroupingExpr(e *Grouping) any
	VisitLiteralExpr(e *Literal) any
	VisitUnaryExpr(e *Unary) any
}

type Binary struct {
	Left     Expr
	Operator token.Token
	Right    Expr
}

func (e *Binary) Accept(v ExprVisitor) any { return v.VisitBinaryExpr(e) }
func (e *Binary) isExpr()                  {}
```

**Why this one:**

- **Compile-time exhaustiveness.** When chapter 8 adds `Variable` and chapter 9
  adds `Logical`, every visitor that doesn't handle the new node stops compiling.
  With a type switch you'd get a runtime panic, discovered whenever you happen to
  execute that branch. You will add ~10 more node types before the book ends;
  this is the payoff.
- **The `any` return costs you almost nothing here.** The interpreter in chapter 7
  returns Lox values, which are `any` in Go regardless. The resolver in chapter 11
  returns nothing. Only the printer wants a `string`, and it can assert once in a
  typed `Print()` wrapper.
- **1:1 with the book,** so you read the chapter instead of translating it.

**The cost:** `any` returns and a type assertion at each visitor's public entry
point. Contain it with a typed wrapper method — never make callers assert.

**On the `isExpr()` marker:** it makes `Expr` a closed set — no type outside
`package ast` can implement it. Free, since the generator writes it.

**Pointers, not values:** all nodes are `*Binary`, `*Unary`, … Consistent method
sets, no copying of trees, and `nil` is a usable "no expression".

---

### Option B — type switch, no visitor

```go
type Expr interface{ isExpr() }

func Print(e Expr) string {
	switch e := e.(type) {
	case *Binary:
		return parenthesize(e.Operator.Lexeme, e.Left, e.Right)
	case *Grouping:
		return parenthesize("group", e.Expression)
	case *Literal:
		return literalString(e.Value)
	case *Unary:
		return parenthesize(e.Operator.Lexeme, e.Right)
	default:
		panic(fmt.Sprintf("ast: unhandled expr type %T", e))
	}
}
```

More idiomatic Go, less ceremony, no `any`, and each new operation is one plain
function. This is genuinely the better-looking code — it is Go's answer to the
expression problem, and it's the same trade-off ML and Haskell make: new
operations are free, new node types mean editing every switch.

The problem is that Go won't tell you which switches you missed. If you take this
route, add [`go-check-sumtype`](https://github.com/BurntSushi/go-sumtype) and tag
the interface with `//sumtype:decl` so a linter recovers the exhaustiveness check.

---

### Option C — generic top-level `Accept` function

```go
func Accept[R any](e Expr, v Visitor[R]) R { ... }  // type-switches internally
```

Recovers typed returns, but the dispatch degrades to a type switch anyway *and*
you keep the visitor boilerplate. Worst of both. Skip it.

---

### Decision

| | A: Visitor | B: Type switch | C: Generic fn |
|---|---|---|---|
| Add a node type | compiler finds every gap | silent runtime panic | silent runtime panic |
| Add an operation | new struct + 4 methods | one function | new struct + 4 methods |
| Return typing | `any` + one assertion | fully typed | typed |
| Idiomatic Go | no | yes | no |
| Matches the book | yes | no | no |

**Go with A.** The safety net matters more than the aesthetics over a
14-chapter build where node types keep arriving. If you'd rather write
idiomatic Go and are willing to run the linter, B is a legitimate choice —
just commit to it now, because switching in chapter 11 is a rewrite of every
consumer.

---

## 5. Package layout

Put the AST in a **top-level `ast/` package**, not under `parser/`:

```
compiler101/
├── ast/
│   ├── expr.go          # GENERATED — do not edit
│   ├── printer.go       # hand-written
│   └── printer_test.go
├── cmd/
│   └── genast/
│       └── main.go      # the generator
└── parser/              # stays empty until chapter 6
```

The book makes the point explicitly: syntax trees "span the border" between the
parser and the interpreter and are "really owned by neither." Concretely, in
chapter 7 your `interpreter` package will import the AST, and
`interpreter → parser/ast` reads as a dependency on the parser that isn't real.
No import cycle either way — this is about what the layout communicates.

If you'd rather mirror the existing `lexer/{token,scanner}` shape, `parser/ast`
works fine. Pick one now; renaming later is a sed across every file.

---

## 6. The AST generator (5.4)

Four node types is little enough to hand-write, and I'd write them by hand
first — you should feel the boilerplate before you automate it. Then build the
generator in the same sitting, delete the hand-written file, and regenerate.

The generator earns itself back fast: chapter 8 adds a whole parallel `Stmt`
hierarchy, and by chapter 13 you have roughly 20 node types × (struct + `Accept`
+ marker + a visitor method each). Hand-editing that after every chapter is how
you end up with a visitor that silently doesn't compile.

### Deviate from the book on the spec format

The book passes type definitions as strings (`"Binary : Expr left, Token operator, Expr right"`)
because Java has no concise literal syntax. Go does — use a struct table and skip
writing a mini-parser:

```go
// cmd/genast/main.go
type field struct{ Name, Type string }
type node  struct {
	Name   string
	Fields []field
}

var exprTypes = []node{
	{"Binary", []field{{"Left", "Expr"}, {"Operator", "token.Token"}, {"Right", "Expr"}}},
	{"Grouping", []field{{"Expression", "Expr"}}},
	{"Literal", []field{{"Value", "any"}}},
	{"Unary", []field{{"Operator", "token.Token"}, {"Right", "Expr"}}},
}
```

The compiler type-checks the table, your editor autocompletes it, and there's no
malformed-spec error path. Adding `Stmt` in chapter 8 is a second slice plus one
more call to `generate`.

### Shape of the generator

```go
func main() {
	out := flag.String("out", ".", "output directory")
	flag.Parse()

	if err := generate(*out, "Expr", exprTypes); err != nil {
		log.Fatal(err)
	}
}

func generate(dir, base string, nodes []node) error {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		Base  string
		Nodes []node
	}{base, nodes}); err != nil {
		return err
	}

	// gofmt the output — lets the template be sloppy about whitespace
	src, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("formatting generated %s: %w\n%s", base, err, buf.String())
	}

	return os.WriteFile(filepath.Join(dir, strings.ToLower(base)+".go"), src, 0o644)
}
```

Key points:

- **`go/format.Source`** on the output. It means your `text/template` can ignore
  indentation entirely, and it fails loudly if the template emits invalid Go.
  When it errors, print the unformatted buffer — otherwise you're debugging blind.
- **Header comment** `// Code generated by cmd/genast. DO NOT EDIT.` as the first
  line. That exact prefix is recognised by `gofmt`, linters, and GitHub diffs.
- **Wire up `go:generate`.** In `ast/gen.go`:
  ```go
  package ast

  //go:generate go run ../cmd/genast -out .
  ```
  Then `go generate ./...` regenerates. Add it to your `Makefile` if you make one.
- **Commit the generated file.** Standard Go practice: `go build` should work on a
  fresh clone without running codegen.

### Template sketch

```go
var tmpl = template.Must(template.New("ast").Parse(`
// Code generated by cmd/genast. DO NOT EDIT.

package ast

import "compiler101/lexer/token"

type {{.Base}} interface {
	Accept(v {{.Base}}Visitor) any
	is{{.Base}}()
}

type {{.Base}}Visitor interface {
{{- range .Nodes}}
	Visit{{.Name}}{{$.Base}}(e *{{.Name}}) any
{{- end}}
}
{{range .Nodes}}
type {{.Name}} struct {
{{- range .Fields}}
	{{.Name}} {{.Type}}
{{- end}}
}

func (e *{{.Name}}) Accept(v {{$.Base}}Visitor) any { return v.Visit{{.Name}}{{$.Base}}(e) }
func (e *{{.Name}}) is{{$.Base}}() {}
{{end}}`))
```

One wrinkle for chapter 8: the `Stmt` hierarchy won't reference `token.Token` in
every node, and an unused import is a compile error in Go. Either always emit the
import and let `Stmt` use `token.Token` somewhere (it will — `Var` has a name
token), or compute the import set from the field types. Cross that bridge then;
don't build it now.

---

## 7. The printer (5.5)

`ast/printer.go` — the first visitor, and your debugging tool for chapter 6.

```go
package ast

import (
	"fmt"
	"strconv"
	"strings"
)

// Printer renders an expression as a parenthesised Lisp-ish string:
//
//	-123 * (45.67)   =>   (* (- 123) (group 45.67))
type Printer struct{}

var _ ExprVisitor = (*Printer)(nil)

// Print is the typed entry point — callers never see the any from Accept.
func (p *Printer) Print(e Expr) string {
	s, _ := e.Accept(p).(string)
	return s
}

func (p *Printer) VisitBinaryExpr(e *Binary) any {
	return p.parenthesize(e.Operator.Lexeme, e.Left, e.Right)
}

func (p *Printer) VisitGroupingExpr(e *Grouping) any {
	return p.parenthesize("group", e.Expression)
}

func (p *Printer) VisitUnaryExpr(e *Unary) any {
	return p.parenthesize(e.Operator.Lexeme, e.Right)
}

func (p *Printer) VisitLiteralExpr(e *Literal) any {
	switch v := e.Value.(type) {
	case nil:
		return "nil"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (p *Printer) parenthesize(name string, exprs ...Expr) string {
	var b strings.Builder
	b.WriteByte('(')
	b.WriteString(name)
	for _, e := range exprs {
		b.WriteByte(' ')
		b.WriteString(p.Print(e))
	}
	b.WriteByte(')')
	return b.String()
}
```

Notes:

- `var _ ExprVisitor = (*Printer)(nil)` — same compile-time assertion you already
  use for `Scanner`. Keep it on every visitor; it's how you find out at build time
  that chapter 8's new node broke this file.
- **Number formatting differs from Java.** The book gets `123.0` from Java's
  `Double.toString`. `strconv.FormatFloat(v, 'f', -1, 64)` gives `123`. Neither is
  wrong — pick one and make your tests agree. You'll write this logic again as
  `stringify` in chapter 7, so consider putting it in one shared helper now.
- **Lox `nil` vs Go `nil`.** A `Literal{Value: nil}` is Lox `nil`. The `case nil:`
  arm catches it. Watch out later: an `any` holding a typed nil pointer is not
  `== nil`. Not an issue for literals, but it will be when values flow through
  the interpreter.

---

## 8. Tests

`ast/printer_test.go`. There is no parser yet, so build the tree by hand — that's
exactly what the book's `main` does, and it's the proof the representation works.

```go
func TestPrinter(t *testing.T) {
	// -123 * (45.67)
	expr := &ast.Binary{
		Left: &ast.Unary{
			Operator: token.Token{Type: token.MINUS, Lexeme: "-", Line: 1},
			Right:    &ast.Literal{Value: 123.0},
		},
		Operator: token.Token{Type: token.STAR, Lexeme: "*", Line: 1},
		Right: &ast.Grouping{
			Expression: &ast.Literal{Value: 45.67},
		},
	}

	got := (&ast.Printer{}).Print(expr)
	if want := "(* (- 123) (group 45.67))"; got != want {
		t.Errorf("Print() = %q, want %q", got, want)
	}
}
```

Then a table-driven test over the interesting cases:

| Input tree | Expected |
|---|---|
| `Literal{nil}` | `nil` |
| `Literal{"hi"}` | `hi` |
| `Literal{123.0}` | `123` (or `123.0` — your call, be consistent) |
| `Grouping{Literal{1.0}}` | `(group 1)` |
| nested binaries | left-associativity is visible in the parens |

Also worth doing: a test that runs the generator into a temp dir and checks the
output is byte-identical to the committed `ast/expr.go`. Catches "someone edited
the generated file by hand", which you will absolutely do at 1am in chapter 9.

---

## 9. Milestones

Sized for one or two sittings.

**M0 — scanner cleanup** ✅ **done**
- [x] All ten items in §2 (EOF token, strings, `/` and comments, `main.go` args, `Run()`, line base, literals, `Token.String()`, `Lex()`)
- [x] `lexer/scanner/scanner_test.go` — 25 tests, 63 subtests, 98.9% coverage
- [x] `go run . script.lox` prints a token list ending in `EOF`

**M1 — grammar written down** ✅ **done**
- [x] `docs/grammar.md` with the expression grammar, the notation legend, and
      the rule → node mapping table
- ✅ You can point at a rule and name the node type it becomes

**M2 — AST by hand** ✅ **done**
- [x] `ast/expr.go` written manually: `Expr`, `ExprVisitor`, the four nodes
- ✅ `go build ./ast/...` clean. 4 nodes × (struct + `Accept` + marker) + 4
      visitor methods. It is as repetitive as advertised — the generator in M4
      reproduced it byte-for-byte modulo comments.

**M3 — printer** ✅ **done**
- [x] `ast/printer.go`
- [x] `ast/value.go` — `Stringify`, the one copy of the number/nil formatting
      rules, shared with the RPN printer now and chapter 7's interpreter later
- [x] `ast/printer_test.go` — the book's tree plus a 24-case table
- ✅ `(* (- 123) (group 45.67))`, and two cases that show the ambiguity of §3
      is visible in the output: `(+ 1 (* 2 3))` vs `(* (+ 1 2) 3)`

**M4 — generator** ✅ **done**
- [x] `cmd/genast/main.go` — spec table as a `hierarchy` struct (so chapter 8's
      `Stmt` is one more value plus one more `generate` call), template,
      `format.Source`
- [x] `ast/gen.go` with the `//go:generate` line
- [x] Deleted the hand-written `expr.go`, ran `go generate ./...`
- [x] `cmd/genast/main_test.go` — golden test against the committed
      `ast/expr.go`, plus a test that an invalid spec fails loudly rather than
      writing a broken file
- ✅ Regenerated file compiles and **the M3 test passed unchanged**
- ✅ Verified the payoff empirically: adding a fifth node type to the spec
      table and regenerating breaks the build in `printer.go` *and* `rpn.go`
      with `missing method VisitTernaryExpr`. That is the §4 argument, observed
      rather than asserted.
- ✅ Verified the golden test catches a hand-edit to `ast/expr.go` and names
      the offending line

**M5 — wire it up** ✅ **done**
- [x] `main.go`: `-ast` flag prints a hand-built tree in both notations.
      Switched arg handling from `os.Args[1:]` to `flag.Args()`; `go run . script.lox`
      behaves as before.
- ✅ ```
      $ go run . -ast
      source :  -123 * (45.67)
      ast    :  (* (- 123) (group 45.67))
      rpn    :  123 negate 45.67 *
      ```

**M6 — challenges** ✅ **done**
- [x] **1. Desugar the metasyntax** — in `docs/grammar.md`: the chapter 5
      grammar with no `|` or `( )`, and the challenge's call/property grammar
      with `*`, `+`, `?` rewritten as recursion.
- [x] **2. The complement to Visitor** — written up in `docs/grammar.md`,
      pointing at §4 for the trade-off. Both halves exist in this repo.
- [x] **3. RPN printer** — `ast/rpn.go` + `ast/rpn_test.go`. Grouping leaves no
      trace (RPN needs no parens); unary minus is spelled `negate`, because
      `123 -` would be indistinguishable from a binary subtraction.

---

## 10. Go gotchas specific to this chapter

- **Methods can't have type parameters.** The reason Option A returns `any`.
  Worth understanding rather than working around.
- **Pointer receivers and interface satisfaction.** With `func (e *Binary) Accept(...)`,
  only `*Binary` implements `Expr` — a bare `Binary{}` does not. Always construct
  with `&`. Consistency here prevents a class of confusing compile errors.
- **Unused imports are errors.** Bites the generator (§6) when a node set doesn't
  reference `token`.
- **`any` is `interface{}`.** Fine to use the alias; you're on Go 1.26.
- **Your `pkg/errors` shadows stdlib `errors`.** Currently harmless. The moment a
  file needs both, you'll be writing `stderrors "errors"`. Consider renaming to
  `pkg/loxerr` or `pkg/report` before that spreads.
- **Global `errors.HadError`** matches the book's static field and is fine for now.
  Chapter 6 adds a parse-error sentinel and chapter 7 adds `HadRuntimeError` —
  when that happens, consider an error-reporter struct passed in, so tests can run
  in parallel without stepping on shared state.
- **`ScannerImpl` naming.** Java-ism. Go convention is `scanner.Scanner` as the
  struct, with an interface only when there are two implementations or a test
  needs to fake it. Not urgent — but don't propagate it to `ParserImpl` in
  chapter 6 without deciding deliberately.

---

## 11. Chapter challenges

Paraphrased from the chapter's Challenges section.

**1. Desugar the metasyntax.** Given a grammar that uses `*`, `+`, `?` and `|`,
rewrite it to an equivalent grammar using none of them. Pure pencil work — the
lesson is that the sugar is only sugar, and that repetition desugars into
recursion. Do it, it's 15 minutes.

**2. The complement to Visitor.** Visitor lets an OO language add operations
easily. Devise the mirror pattern for a functional language: bundle all operations
on one *type* together, so adding a new type is easy. **In Go you have already
seen both halves** — Option A is Visitor, Option B (type switch) is the functional
side. Writing up why each makes one axis cheap is the best possible preparation
for the layout decision in §4.

**3. RPN printer.** Write a second visitor that emits reverse Polish notation
(`(1 + 2) * (4 - 3)` → `1 2 + 4 3 - *`). This is the real deliverable of the
chapter: a second operation over the same tree, with zero changes to the node
types. That's the whole argument for the pattern, and it takes ~20 lines.
Put it in `ast/rpn.go` with its own test.

---

## 12. What chapter 6 will need from you

Chapter 6 (Parsing Expressions) starts immediately after, so leave these in place:

- ✅ **An `EOF` token at the end of every token slice.** Done, and pinned by
  `TestScanTokensAlwaysEndsWithEOF` across empty, whitespace-only and
  comment-only input. The parser's `isAtEnd()` depends on it.
- **`Expr` node types stable and generated.** Chapter 6 constructs them; it
  doesn't change them.
- **The printer working.** It is how you'll verify that `1 + 2 * 3` parses as
  `(+ 1 (* 2 3))` and not `(* (+ 1 2) 3)`. Without it you're reading `%+v` dumps
  of nested pointers.
- **Token-aware error reporting.** Chapter 6 needs
  `errors.TokenError(tok token.Token, msg string)` that reports `at end` for EOF
  and `at '<lexeme>'` otherwise, plus a `ParseError` sentinel to unwind on.
  You can add it now or when you get there — but know it's coming, and it doesn't
  fit the current `Error(line, message)` signature.
- **The grammar file**, because chapter 6 rewrites it into the precedence ladder
  (`equality → comparison → term → factor → unary → primary`) and you'll want to
  diff against the ambiguous version.
