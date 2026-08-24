# Chapter 7 — Evaluating Expressions, in Go

Plan for porting [craftinginterpreters.com/evaluating-expressions.html](https://craftinginterpreters.com/evaluating-expressions.html)
to this repo (`module compiler101`, Go 1.26).

**Goal of the chapter:** the tree stops being a picture and starts being a
program. A second visitor over `ast.Expr` — this one returning *values* instead
of strings — turns `1 + 2 * 3` into `7`, and `-"muffin"` into a *runtime* error
that names the line. After this chapter `go run . script.lox` prints answers.

> Status: implemented through M6. The optional challenges in §12 remain open.
> Every Go claim in §11 was compiled and run before being written down.
> The implementation lives in `interpreter/{interpreter.go,interpreter_test.go}`,
> with additions to `pkg/errors/errors.go` and `main.go`. The `ast/` package
> needed **no** changes — `ast.Stringify` was already written for this chapter.

---

## Contents

1. [Where the repo is now](#1-where-the-repo-is-now)
2. [What the chapter delivers](#2-what-the-chapter-delivers)
3. [Design decision: how a runtime error escapes a visitor that returns `any`](#3-design-decision-how-a-runtime-error-escapes-a-visitor-that-returns-any)
4. [Package layout](#4-package-layout)
5. [Runtime values](#5-runtime-values-71)
6. [Runtime errors in `pkg/errors`](#6-runtime-errors-in-pkgerrors-73)
7. [The interpreter](#7-the-interpreter-72)
8. [Wiring `main.go`](#8-wiring-maingo-74)
9. [Tests](#9-tests)
10. [Milestones](#10-milestones)
11. [Go gotchas specific to this chapter](#11-go-gotchas-specific-to-this-chapter)
12. [Chapter challenges](#12-chapter-challenges)
13. [What chapter 8 will need](#13-what-chapter-8-will-need-from-you)

---

## 1. Where the repo is now

```
compiler101/
├── main.go                       # CLI: Run / RunFile / RunPrompt          ✅
├── ast/
│   ├── expr.go                   # generated: Expr, ExprVisitor, 4 nodes   ✅
│   ├── printer.go                # Printer  — (* (- 123) (group 45.67))    ✅
│   ├── rpn.go                    # RPNPrinter                              ✅
│   ├── value.go                  # Stringify(any) string                   ✅
│   └── gen.go                    # go:generate directive                   ✅
├── lexer/…                       # chapter 4                               ✅
├── parser/
│   ├── parser.go                 # recursive descent (ch. 6)               ✅
│   ├── algorithm.go              # Algorithm iface, Kind, Config, NewOf     ✅
│   └── llk/…                     # table-driven LL(k) — second front end   ✅
├── pkg/errors/errors.go          # HadError, Error, ErrorToken, ParseError  ✅
└── interpreter/                  # empty — this chapter lands here
```

Three things already in place matter to this chapter, and are worth naming
before writing any code:

- **`ast.Stringify`** (`ast/value.go`) was written *for* this chapter. Its doc
  comment says so. The interpreter must not grow a second copy of number
  formatting — §5.
- **`ast.ExprVisitor` returns `any`.** That is generated code, and Go cannot put
  type parameters on methods, so the signature is fixed. Everything in §3
  follows from that one constraint.
- **`parser.Algorithm`** hides which parser ran. The interpreter takes an
  `ast.Expr` and must stay unable to tell — which also gives a free parity test
  (§9).

---

## 2. What the chapter delivers

| Piece | Book | Here |
|---|---|---|
| Value representation | `Object` (`null`/`Boolean`/`Double`/`String`) | `any` (`nil`/`bool`/`float64`/`string`) |
| Evaluator | `Interpreter implements Expr.Visitor<Object>` | `*interpreter.Interpreter` implements `ast.ExprVisitor` |
| Recursion helper | `evaluate(expr)` | `i.evaluate(e ast.Expr) any` |
| Truthiness | `isTruthy` | `truthy(v any) bool` |
| Equality | `isEqual` | `equal(a, b any) bool` |
| Operand checks | `checkNumberOperand(s)` | `i.number(tok, v)` / `i.numbers(tok, a, b)` |
| Error unwinding | `throw new RuntimeError` | `panic(runtimeSignal{…})` + one `recover` — §3 |
| Entry point | `interpret(expr)` prints | `Interpret(expr) (any, error)`; the caller prints |
| Error reporting | `Lox.runtimeError`, `hadRuntimeError` | `errors.ReportRuntimeError`, `errors.HadRuntimeError` |
| Exit code | 70 | 70 (`EX_SOFTWARE`), alongside the existing 65 |

One deliberate divergence from the book's shape: **`Interpret` returns the
value, it does not print it.** The book prints inside the interpreter because
`main` is the only caller; here the interpreter would then be untestable without
capturing stdout, and `main.go` already owns every `fmt.Println` in the
pipeline. Printing stays in `main.go`, via `ast.Stringify`.

---

## 3. Design decision: how a runtime error escapes a visitor that returns `any`

This is the whole chapter's Go problem. `VisitBinaryExpr` is three levels deep
in a recursion, discovers `1 + "a"`, and has to abandon the entire tree. Java
throws. Go has no exceptions, and the method signature — generated, and shared
with `Printer` and `RPNPrinter` — is `Accept(v ExprVisitor) any`, with nowhere
to put an `error`.

Three ways out.

**A. `panic` inside, one `recover` at the package boundary.** ← recommended

```go
// runtimeSignal is how a runtime error leaves a Visit method. The visitor
// signature is generated and returns a bare any, so there is no error to
// return; panic is the only way to abandon a half-built recursion. The type is
// unexported and never escapes: Interpret recovers it and hands the caller a
// plain error, so "panics as control flow" is contained to this one package.
type runtimeSignal struct{ err *errors.RuntimeError }
```

- Reads exactly like the book, line for line, which matters when the next six
  chapters are diffs against the book.
- No error plumbing in the 4 (soon 20) visit methods.
- A panic that is *not* a `runtimeSignal` — nil map write, index out of range,
  a genuine bug in the interpreter — is re-panicked, so it still crashes loudly
  instead of being laundered into "Operand must be a number."
- Cost: `panic` as control flow. Acceptable precisely because it cannot be
  observed from outside the package.

**B. A sticky `err` field on `Interpreter`, checked after every `evaluate`.**

```go
func (i *Interpreter) evaluate(e ast.Expr) any {
    if i.err != nil { return nil } // already failed; stop doing work
    return e.Accept(i)
}
```

No panic, but every visit method needs a guard, and every operand check must
also guard — otherwise the `nil` left behind by the first failure reaches
`i.numbers` and reports a *second*, bogus error at the wrong token. That is a
correctness trap that reappears with every node type added from chapter 8 on,
and the guards outnumber the code they protect.

**C. Change the visitor signature to `(any, error)`.**

Honest Go, and wrong here: it edits generated code, drags `Printer`,
`RPNPrinter` and their tests along, and puts `if err != nil { return nil, err }`
in front of every recursive call in a file that is supposed to read like a
grammar. Chapter 10's `Return` unwinds the same way and would need the same
machinery anyway.

**Decision: A.** Panic in, error out, non-runtime panics re-thrown. Revisit only
if the interpreter ever needs to *resume* after an error mid-tree, which Lox
never does.

---

## 4. Package layout

```
interpreter/
├── interpreter.go        # Interpreter, Interpret, evaluate, visits, helpers
└── interpreter_test.go   # package interpreter_test — exported API only
pkg/errors/errors.go      # + RuntimeError, HadRuntimeError, ReportRuntimeError
main.go                   # + -print flag, evaluate instead of print, exit 70
```

A new top-level `interpreter/` package, sibling to `lexer/` and `parser/`, not a
file inside `ast/`. Reasons: `ast` must stay dependency-free so both printers
and the generator keep working; the interpreter will need `Environment` (ch. 8),
`LoxFunction` (ch. 10) and `LoxClass` (ch. 12), all of which belong next to it;
and a separate package forces the "exported API only" test style the repo
already uses in `ast_test`.

Import direction stays a DAG: `main → interpreter → {ast, lexer/token,
pkg/errors}`. No cycle, and `pkg/errors` still sits below everything, which is
why `RuntimeError` goes there and not in `interpreter` (§6).

---

## 5. Runtime values (7.1)

Nothing to build. The mapping is already what the parser produces:

| Lox | Go dynamic type | Produced by |
|---|---|---|
| `nil` | `nil` | `&ast.Literal{Value: nil}` |
| `true` / `false` | `bool` | `&ast.Literal{Value: true}` |
| number | `float64` | scanner's `strconv.ParseFloat` |
| string | `string` | scanner |

Two consequences worth stating out loud because tests depend on them:

- **All numbers are `float64`.** `7 / 2` is `3.5`, not `3`. There is no integer
  type to fall back to, and no integer-division panic — §11.5.
- **Printing is `ast.Stringify`, and only `ast.Stringify`.** The book's
  `stringify` strips a trailing `".0"` because Java's `Double.toString` adds it;
  Go's `FormatFloat(v, 'f', -1, 64)` never does, so the repo's version is
  already the whole answer. If the interpreter grows its own formatter, the
  printer tests and the eval tests will disagree about `123` vs `123.0` and one
  of them will be quietly wrong.

---

## 6. Runtime errors in `pkg/errors` (7.3)

`RuntimeError` goes beside `ParseError`, for the reason already written in that
file: the shared error type has to sit below every package that produces or
consumes it. `main.go` needs it for the exit code, `interpreter` produces it.

```go
// HadRuntimeError is the runtime twin of HadError. It is separate because the
// exit codes differ — 65 for source the parser rejected, 70 for source that
// parsed and then blew up — and RunFile has to tell them apart.
var HadRuntimeError bool

// RuntimeError is a failure discovered while evaluating, located at the
// operator token that failed. The token is the whole point: "Operands must be
// numbers." is useless without a line number.
type RuntimeError struct {
    Token   token.Token
    Message string
}

func (e *RuntimeError) Error() string {
    return fmt.Sprintf("line %d, at %q: %s", e.Token.Line, e.Token.Lexeme, e.Message)
}

// ReportRuntimeError prints e for the user and records that it happened. Same
// two-jobs-one-call shape as ParseErrorAt, and the same reason: every caller
// wants both halves.
func ReportRuntimeError(e *RuntimeError) {
    fmt.Fprintf(os.Stderr, "%s\n[line %d]\n", e.Message, e.Token.Line)
    HadRuntimeError = true
}
```

Also update `Reset` — one line, easy to forget, and forgetting it means the
first bad REPL line poisons the exit code of the whole session:

```go
func Reset() {
    HadError = false
    HadRuntimeError = false
}
```

Two small decisions inside this section:

- **Message format** follows the book (`msg` then `[line N]` on its own line)
  rather than the repo's `[line N] Error: msg`. The book's format is what every
  worked example in chapters 8–13 shows, and matching it keeps future
  copy-pasted expectations honest. If you would rather be internally
  consistent, use `report(e.Token.Line, "", e.Message)` instead and note it here
  — just decide once, because tests will assert on it.
- **`Error()` mirrors `ParseError.Error()`** so a caller printing `%v` gets
  something sane, even though the user-facing text comes from
  `ReportRuntimeError`.

---

## 7. The interpreter (7.2)

Full sketch. It compiles against the current `ast` package as written.

```go
// Package interpreter walks an ast.Expr and produces a value.
//
// It is the second visitor over the tree — ast.Printer was the first — and the
// first one whose result can fail: -"muffin" is a perfectly good parse and a
// runtime error. How that error gets out of the recursion is the one Go-shaped
// decision in the package; see docs/07-evaluating-expressions.md §3.
package interpreter

// Interpreter evaluates expressions. It holds no state yet — chapter 8 adds the
// environment — but it is a struct, not a bare function, because everything
// from here on hangs off it.
type Interpreter struct{}

// Compile-time proof of coverage, same guard ast.Printer uses. When chapter 8
// generates a new node type, this line is what fails the build.
var _ ast.ExprVisitor = (*Interpreter)(nil)

func New() *Interpreter { return &Interpreter{} }

// Interpret is the typed entry point: the only place that turns the visitor's
// any into a value plus an error, and the only place that recovers.
func (i *Interpreter) Interpret(e ast.Expr) (value any, err error) {
    defer func() {
        r := recover()
        if r == nil {
            return
        }
        sig, ok := r.(runtimeSignal)
        if !ok {
            panic(r) // not ours: a real bug. Let it kill the process.
        }
        value, err = nil, sig.err
    }()
    return i.evaluate(e), nil
}

func (i *Interpreter) evaluate(e ast.Expr) any { return e.Accept(i) }

// fail is the throw. It never returns, which is why call sites can use its
// result positionally without a follow-up return.
func (i *Interpreter) fail(t token.Token, message string) {
    panic(runtimeSignal{err: &errors.RuntimeError{Token: t, Message: message}})
}

// -------------------------------------------------------------- the visits --

func (i *Interpreter) VisitLiteralExpr(e *ast.Literal) any { return e.Value }

// A Grouping contributes nothing at runtime — it did its job in the parser, by
// changing which tree got built.
func (i *Interpreter) VisitGroupingExpr(e *ast.Grouping) any {
    return i.evaluate(e.Expression)
}

func (i *Interpreter) VisitUnaryExpr(e *ast.Unary) any {
    right := i.evaluate(e.Right)

    switch e.Operator.Type {
    case token.BANG:
        return !truthy(right)
    case token.MINUS:
        return -i.number(e.Operator, right)
    }
    // The parser only builds ! and - here. Reaching this is a parser bug, and a
    // located error message beats returning a silent nil that shows up three
    // frames later as "nil is not a number".
    i.fail(e.Operator, "Unsupported unary operator.")
    return nil
}

func (i *Interpreter) VisitBinaryExpr(e *ast.Binary) any {
    // Both operands are evaluated, left first, before either is type-checked.
    // That ordering is observable once chapter 8 gives expressions side
    // effects, so it is a promise, not an accident.
    left := i.evaluate(e.Left)
    right := i.evaluate(e.Right)

    switch e.Operator.Type {
    case token.MINUS:
        l, r := i.numbers(e.Operator, left, right)
        return l - r
    case token.SLASH:
        l, r := i.numbers(e.Operator, left, right)
        return l / r // see §12.3 — this is where divide-by-zero is decided
    case token.STAR:
        l, r := i.numbers(e.Operator, left, right)
        return l * r

    case token.PLUS:
        // The one overloaded operator: numbers add, strings concatenate, and
        // mixing them is an error rather than a coercion (§12.2).
        switch l := left.(type) {
        case float64:
            if r, ok := right.(float64); ok {
                return l + r
            }
        case string:
            if r, ok := right.(string); ok {
                return l + r
            }
        }
        i.fail(e.Operator, "Operands must be two numbers or two strings.")

    case token.GREATER:
        l, r := i.numbers(e.Operator, left, right)
        return l > r
    case token.GREATER_EQUAL:
        l, r := i.numbers(e.Operator, left, right)
        return l >= r
    case token.LESS:
        l, r := i.numbers(e.Operator, left, right)
        return l < r
    case token.LESS_EQUAL:
        l, r := i.numbers(e.Operator, left, right)
        return l <= r

    case token.BANG_EQUAL:
        return !equal(left, right)
    case token.EQUAL_EQUAL:
        return equal(left, right)
    }

    i.fail(e.Operator, "Unsupported binary operator.")
    return nil
}

// ------------------------------------------------------------------ helpers --

// truthy is Lox's rule, and it is one sentence: false and nil are falsey,
// everything else — 0, "", "false" — is truthy.
func truthy(v any) bool {
    switch v := v.(type) {
    case nil:
        return false
    case bool:
        return v
    default:
        return true
    }
}

// equal compares two runtime values. Go's == on two any values does the right
// thing for every type Lox has (nil, bool, float64, string are all comparable),
// so there is no reflect here — but that safety depends on the value set: the
// day a Lox value is backed by a slice or a map, == panics instead of
// answering. See §11.3.
func equal(a, b any) bool {
    if a == nil || b == nil {
        return a == nil && b == nil
    }
    return a == b
}

// number asserts a single operand, for unary -.
func number(…) // i.number(t token.Token, v any) float64
// numbers asserts both, for arithmetic and comparison.
func numbers(…) // i.numbers(t token.Token, a, b any) (float64, float64)
```

with

```go
func (i *Interpreter) number(t token.Token, v any) float64 {
    n, ok := v.(float64)
    if !ok {
        i.fail(t, "Operand must be a number.")
    }
    return n
}

func (i *Interpreter) numbers(t token.Token, a, b any) (float64, float64) {
    l, lok := a.(float64)
    r, rok := b.(float64)
    if !lok || !rok {
        i.fail(t, "Operands must be numbers.")
    }
    return l, r
}
```

Note the two helpers are methods, not free functions, purely so they can call
`i.fail`. That is also why the `default` branches say `return nil` after a call
that never returns: the compiler cannot know `fail` always panics.

---

## 8. Wiring `main.go` (7.4)

Three changes.

**1. `Run` evaluates instead of printing the tree.** The chapter-6 output is
still worth keeping — it is how you debug precedence — so it moves behind a
flag rather than being deleted:

```go
type options struct {
    parser parser.Config
    tokens bool
    show   string // "eval" (default), "ast", or "rpn"
}
```

```go
show := flag.String("print", "eval", "what to print: eval, ast, or rpn")
```

and one place that turns an `ast.Expr` into a line of output:

```go
// emit prints one parsed expression the way -print asked for. Evaluating is the
// default now; the two printers are debugging views of the same tree.
func emit(interp *interpreter.Interpreter, expr ast.Expr, show string) {
    switch show {
    case "ast":
        fmt.Println((&ast.Printer{}).Print(expr))
    case "rpn":
        fmt.Println((&ast.RPNPrinter{}).Print(expr))
    default:
        value, err := interp.Interpret(expr)
        var rte *errors.RuntimeError
        if goerrors.As(err, &rte) {
            errors.ReportRuntimeError(rte)
            return
        }
        fmt.Println(ast.Stringify(value))
    }
}
```

Both the single-expression path and the `ParseAll` batch path call `emit`. One
`*interpreter.Interpreter` is created per `Run` — per *session*, once chapter 8
gives it state, so it must not be created per expression.

**2. Batch mode stops at the first runtime error.** A syntax error costs one
message and the parser resynchronises; a runtime error does not, because the
state after it is undefined. Concretely: in the `containsSemicolon` loop, break
once `errors.HadRuntimeError` is set.

**3. Exit codes.** `RunFile` gains the second check, ordered so a syntax error
still wins:

```go
if errors.HadError {
    os.Exit(65) // EX_DATAERR — it never ran
}
if errors.HadRuntimeError {
    os.Exit(70) // EX_SOFTWARE — it ran and failed
}
```

`RunPrompt` needs nothing: it already calls `errors.Reset()` per line, which now
clears the runtime flag too, so a REPL survives `1 + "a"` and keeps going. That
is the behaviour the chapter explicitly wants.

**One wart to decide.** The existing `-ast` boolean flag prints the hand-built
chapter-5 sample tree, which now reads as if it were `-print=ast`. Either rename
it to `-sample` (a two-line change, and the honest name — `printSampleAST` is
what it calls), or leave it and accept the overlap. Renaming is recommended.

---

## 9. Tests

New file `interpreter/interpreter_test.go`, `package interpreter_test`, driving
the real pipeline — `lexer.Lex` → `parser.New` → `Interpret` — because that is
the only way to test what a user actually types, and hand-built trees are
already covered in `ast_test`.

```go
// eval is the whole pipeline, so a test case is a string of Lox and an answer.
func eval(t *testing.T, source string) (string, error) {
    t.Helper()
    expr, err := parser.New(lexer.Lex(source)).Parse()
    if err != nil {
        t.Fatalf("parse(%q): %v", source, err)
    }
    v, err := interpreter.New().Interpret(expr)
    return ast.Stringify(v), err
}
```

**Value cases** (source → expected `Stringify`):

| Source | Want | Why it is in the table |
|---|---|---|
| `1`, `"hi"`, `true`, `nil` | `1`, `hi`, `true`, `nil` | literals pass through |
| `1 + 2 * 3` | `7` | precedence survived the parser |
| `(1 + 2) * 3` | `9` | `Grouping` is honoured |
| `-3`, `- -3` | `-3`, `3` | unary minus, nested |
| `7 / 2` | `3.5` | numbers are float64, §5 |
| `"a" + "b"` | `ab` | `+` overload, string arm |
| `!true`, `!nil`, `!0`, `!""` | `false`, `true`, `false`, `false` | truthiness — `0` and `""` are **truthy** |
| `1 < 2`, `2 <= 2`, `3 > 4` | `true`, `true`, `false` | comparisons yield bools |
| `1 == 1`, `nil == nil`, `1 == "1"`, `true != false` | `true`, `true`, `false`, `true` | equality across and within types |

**Error cases** (source → message, and the line the token carries):

| Source | Message |
|---|---|
| `-"a"` | `Operand must be a number.` |
| `!` … n/a | (parser's problem, not ours) |
| `1 + "a"` | `Operands must be two numbers or two strings.` |
| `"a" * 2`, `1 < "a"`, `nil - 1` | `Operands must be numbers.` |

Assert with `errors.As` on `*errors.RuntimeError`, and check `Token.Line` too —
a message with the wrong line is the bug this chapter is most likely to ship.

**Four tests beyond the tables:**

1. **Front-end parity.** Same sources through `parser.NewOf` with `Kind: RD`
   and `Kind: LLK`, asserting identical values. The `Algorithm` interface claims
   the interpreter cannot tell them apart; this is the test that says so.
2. **REPL continuity.** Interpret a failing expression, then a good one on the
   same `*Interpreter`, and assert the second succeeds. Cheap now, load-bearing
   from chapter 8 when the interpreter holds an environment.
3. **Panics are not laundered.** Build a `&ast.Binary{Operator: {Type:
   token.COMMA}}` by hand and assert it comes back as a `*RuntimeError`
   ("Unsupported binary operator."), not a crash — the `default` branches in §7.
4. **Left-to-right evaluation order.** Weak until chapter 8 has side effects,
   but `1 + "a"` vs `"a" + 1` should both report at the `+` token, not at a
   literal.

**Housekeeping:** any test that reads `errors.HadRuntimeError` cannot call
`t.Parallel()` — the flag is global, and `errors.go` already documents that rule
for `HadError`. Reset it with `t.Cleanup(errors.Reset)`. The table tests above
touch no globals (they use the returned `error`) and can stay parallel.

**Gates before calling the chapter done:** `go test ./...` green, `gofmt -l .`
and `go vet ./...` clean, `go generate ./...` a no-op against the committed tree
(this chapter must not touch `ast/expr.go`), and `interpreter/` at or near 100%
statement coverage — it is a switch statement, there is no excuse.

---

## 10. Milestones

Each one compiles and has tests. `M1`–`M4` never touch `main.go`, so the CLI
keeps working the whole way through.

| # | Deliverable | Done when |
|---|---|---|
| **M1** | `pkg/errors`: `RuntimeError`, `HadRuntimeError`, `ReportRuntimeError`, `Reset` update | `go build ./...`; a unit test asserts the printed format and that `Reset` clears both flags |
| **M2** | `interpreter` skeleton: struct, `var _ ast.ExprVisitor`, `Interpret` + `recover`, `evaluate`, `fail`, literal + grouping | `1`, `"hi"`, `(nil)` evaluate; all four visit methods exist (stubs may `fail`) |
| **M3** | `truthy`, `number`, unary `!` and `-` | `!nil`, `-3` pass; `-"a"` returns a `*RuntimeError` at the right line |
| **M4** | `numbers`, all of `VisitBinaryExpr`, `equal` | the full value table and error table in §9 pass |
| **M5** | `main.go`: `-print` flag, `emit`, batch break, exit 70, `-ast` → `-sample` | `go run . -print=ast` reproduces chapter 6 output; a script with `1+"a";` exits 70; a script with `1+;` still exits 65 |
| **M6** | Parity, REPL-continuity, panic and order tests; coverage; `gofmt`/`vet`/`generate` gates | §9 housekeeping all green |
| **M7** | Challenges — §12. Optional, and each one is a decision to write down before it is code | recorded in this file, with tests for whatever ships |

Suggested commit shape, one per milestone, so `feat(interpreter): evaluate
expressions` does not arrive as a single 400-line diff.

---

## 11. Go gotchas specific to this chapter

Everything below was run before being written down.

1. **`recover()` only works in a function deferred directly by the frame that
   panicked through.** And to *replace* the return value you need **named
   results** — `(value any, err error)`. With anonymous results the `recover`
   swallows the panic and `Interpret` returns the zero values silently, which
   looks exactly like "it evaluated to nil".

2. **Re-panic what is not yours.** `if !ok { panic(r) }`. Without that line, an
   index-out-of-range bug in the interpreter becomes a nil value with a nil
   error, and you will spend an evening on it.

3. **`==` on two `any` values panics for uncomparable dynamic types** —
   verified: `comparing uncomparable type []int`. It is safe here *only*
   because every Lox value is `nil`, `bool`, `float64` or `string`. Chapter 12's
   class instances are pointers (still comparable), but the moment a value is
   backed by a slice or map, `equal` needs a guard.

4. **NaN equality diverges from the book.** Java's `Double.equals` says NaN
   equals NaN, so book-Lox answers `true` for `(0/0) == (0/0)`. Go's `==`
   follows IEEE 754 and answers `false` (verified). Ours is the more defensible
   answer; it is a divergence, so it belongs in a test with a comment rather
   than being discovered later as a bug.

5. **Float division by zero does not panic.** Verified: `1/0` → `+Inf`, `-1/0` →
   `-Inf`, `0/0` → `NaN`. Only *integer* division panics, and Lox has no
   integers. So challenge 3 is entirely a policy decision — nothing stops you.

6. **`ast.Stringify` on those values prints `+Inf`, `-Inf`, `NaN`, `-0`**
   (verified), and formats `1e21` as `1000000000000000000000` because
   `FormatFloat` is called with `'f'`. `'g'` would give `1e+21`. Either is fine;
   what is not fine is discovering it from a failing test after §12.3 makes
   `Inf` reachable.

7. **Do not write a test expecting `0.1 + 0.2` to print `0.3`.** In Go *source*,
   `0.1+0.2` is constant-folded at arbitrary precision and formats as `0.3`; the
   same expression through the scanner's `ParseFloat` is
   `0.30000000000000004` (both verified). The interpreter takes the second path.

8. **No operator overloading, so no `-any`.** Every arithmetic operator needs
   its operands asserted to `float64` first. That is what `number`/`numbers`
   are, and why they return the converted values rather than a `bool`.

9. **A function that always panics still needs a `return` after it.** Go's
   terminating-statement analysis does not look inside `i.fail`. Hence the
   `return nil` at the end of `VisitUnaryExpr` and `VisitBinaryExpr`, and hence
   `numbers` returning zero values it will never actually return.

10. **Type switch on `nil` works: `case nil:` in a type switch matches an `any`
    holding no value** — that is what makes `truthy` a clean three-arm switch.
    But `case nil:` in a *value* switch on `any` does not do the same job; keep
    it a type switch.

11. **Globals mean no `t.Parallel()`** for tests reading `HadError` /
    `HadRuntimeError`, as `pkg/errors` already warns. Prefer asserting on the
    returned `error` so tests stay parallel.

---

## 12. Chapter challenges

**1. Comparisons on non-numbers.** *Recommendation: strings yes, mixed types
no.* `"apple" < "banana"` has one obvious meaning (Go's `<` on strings is
byte-wise lexicographic, so it comes free), and it makes sorting a
heterogeneous-free list of strings possible. `3 < "pancake"` has no meaning
anyone would agree on; JavaScript's answer to that family of questions is the
cautionary tale, and Python 3 removing Python 2's cross-type ordering is the
correction. Implementation: in the four comparison cases, try `float64`/`float64`
first, then `string`/`string`, and fail otherwise — the same shape as `+`. Test
`"a" < "b"`, `"b" < "a"`, `"a" < 1` → error. Note the honest cost: comparison
messages become `Operands must be two numbers or two strings.` too.

**2. `+` coerces when either side is a string.** Mechanical: add a third arm
before the failure, `if _, ok := left.(string); ok { return ast.Stringify(left) +
ast.Stringify(right) }` and the mirror. Reuse `ast.Stringify` so `"scone" + 4`
gives `scone4` and not `scone4.0` — this is exactly why the formatter lives in
one place. *Recommendation: implement it, then decide whether to keep it.* It
makes `"total: " + n` pleasant and `a + b` untrustworthy in equal measure; if
you keep it, keep it behind a named flag or at least a comment saying which way
you chose, because chapter 8's tests will bake in the answer.

**3. Divide by zero.** Right now (§11.5) it silently yields `+Inf`/`NaN`, which
then prints as `+Inf` — a value no Lox program can produce a literal for and no
Lox operator can usefully consume. *Recommendation: raise a runtime error* at
the `/` token, message `Division by zero.`, guarded as `if r == 0 { i.fail(…) }`
before the divide. Rationale: Lox has one numeric type and no way to talk about
infinity, so `Inf` is a value that can only propagate and confuse; erroring
turns a silent wrong answer into a located message, which is the entire theme of
this chapter. The alternatives are real, though — C and Java hand back IEEE
infinities because the hardware does and the language has `Double.isInfinite`
to inspect them; Python raises `ZeroDivisionError` because it would rather stop
than hand you a value you did not ask for. Lox is closer to Python here.
Test `1 / 0`, `-1 / 0`, `0 / 0` all error, and `1 / 0.5` → `2`.

---

## 13. What chapter 8 will need from you

- **`Interpreter` as a struct with state.** Chapter 8 adds `Environment` to it,
  and `main.go` must already be creating one per session, not one per
  expression. §8 does this; do not "simplify" it back to a package-level
  function.
- **`interpret` taking a list.** `Interpret(expr)` becomes
  `Interpret(stmts []ast.Stmt)`. Keep the single-expression form as
  `evaluateExpr` or similar for the REPL and for tests — the value tables in §9
  are worth keeping working.
- **A second generated hierarchy.** `cmd/genast` already models this: `Stmt` is
  a second `hierarchy` value plus one more call to generate. The
  `var _ ast.ExprVisitor = (*Interpreter)(nil)` line gains a
  `var _ ast.StmtVisitor` twin, and that pair is what will tell you at compile
  time that you missed a node.
- **The `;` hack in `main.go` goes away.** `containsSemicolon` exists because
  chapter 6 had no statements. Chapter 8 has them, and `Run` becomes
  unconditionally "parse a program, execute it". `-print=ast` should survive the
  change.
