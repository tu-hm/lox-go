# The front end — from source text to syntax tree

Everything before evaluation. This doc is the overview: what the lexer and the
parser each do, why they are two passes and not one, and what they promise each
other. The per-chapter docs go deeper on individual features; this one is the map.

Read it alongside the code. Every claim here was checked against the running
program, and the commands to re-check them are in the text.

```
source text  ──lexer──▶  []token.Token  ──parser──▶  []ast.Stmt  ──▶ resolver ──▶ interpreter
   string                 flat, EOF-           tree, no
                          terminated           tokens left
```

Three passes, three shapes. Each one throws away something the next does not
need, and that discarding is the whole design.

| | input | output | what it discards |
|---|---|---|---|
| lexer | a `string` | flat token slice | whitespace, comments, character-level detail |
| parser | token slice | statement trees | punctuation, precedence (now in the shape) |
| resolver | trees | trees + a side table | nothing; it *adds* (distance, slot) |

You can watch each stage:

```sh
env GOTOOLCHAIN=go1.26.6 go run . -tokens      script.lox   # stop after the lexer
env GOTOOLCHAIN=go1.26.6 go run . -print=ast   script.lox   # stop after the parser
env GOTOOLCHAIN=go1.26.6 go run . -print=rpn   script.lox   # the same tree, other notation
```

---

# Part 1 — The lexer

Files: [`lexer/lexer.go`](../lexer/lexer.go),
[`lexer/scanner/scanner.go`](../lexer/scanner/scanner.go),
[`lexer/token/token.go`](../lexer/token/token.go),
[`utils/utils.go`](../utils/utils.go).

## Why it is a separate pass

It does not have to be. You could have the parser read characters directly. The
reason not to is that the two jobs need different amounts of memory about the
past.

Recognising `123` as a number needs to know only "I am partway through digits".
Recognising `(1 + 2) / 2` as a division needs to know how deeply nested you are,
which is unbounded. Splitting them means the character-level code is a flat loop
with three integers of state, and the nesting problem is solved once, in the
parser, over a much smaller alphabet.

The practical payoff: the parser never sees whitespace, never sees a comment,
and never has to ask whether `<=` is one operator or two. All of that is settled
before it starts.

## The scanning loop

`ScanToken` is the whole thing:

```go
for !s.isAtEnd() {
    s.start = s.current   // remember where this token began
    s.scanToken()         // consume exactly one token, or report and continue
}
// then append EOF, always
```

Three integers of state, and they are worth naming precisely:

- `start` — index where the current token begins. Only used to slice out the
  lexeme at the end: `s.Source[s.start:s.current]`.
- `current` — the read head. `advance()` returns the byte and moves it.
- `line` — for error messages. Incremented at `\n`, including inside strings.

There is no backtracking. `current` only ever moves forward, which is why the
scanner is linear in the length of the source and why no token can be
"un-read".

## One character, or two?

The interesting case is an operator that is a prefix of a longer operator:

```go
case '!':
    if s.match('=') { s.addToken(token.BANG_EQUAL) } else { s.addToken(token.BANG) }
```

`match` is a conditional advance: it consumes the next byte only if it is the
one you asked for. This is **maximal munch** — when two rules could match, the
one consuming more characters wins. Verified:

```
<=    →  LESS_EQUAL
< =   →  LESS, EQUAL
```

The same rule is why `orchid` is one identifier and not `or` followed by
`chid`, which is the subject of a later section.

`/` is the odd one out: a second `/` does not make a longer operator, it starts
a comment, and the scanner consumes to end of line and emits nothing.

## Literals are the only tokens carrying a value

A `Token` is four fields:

```go
type Token struct {
    Type    TokenType   // NUMBER, PLUS, IDENTIFIER, …
    Lexeme  string      // the exact source text
    Literal any         // the *value*, for NUMBER and STRING only
    Line    int
}
```

`Literal` is `any` and is nil for every token type except `NUMBER` and `STRING`.
That nil is load-bearing: the interpreter's `Literal` node stores `Value any` and
uses `nil` to mean Lox `nil`, so a token type that carried an accidental zero
value would be indistinguishable from a real one. The scanner has a two-function
split to keep this honest — `addToken` always passes nil, `addTokenWithValue` is
the only way to attach one.

Numbers are parsed to `float64` here, once, rather than re-parsed downstream.
Lox has exactly one number type; `1` and `1.0` are the same value, which is why
`ast.Stringify` prints `1` rather than Java's `1.0`.

Strings keep their quotes in `Lexeme` and drop them in `Literal`:

```
STRING  "café"  café
```

Lox strings may span newlines, which is why `stringLiteral` bumps `line` as it
scans. They have no escape sequences at all — `\n` inside a Lox string is a
backslash and an `n`.

## Identifiers and keywords: scan first, look up second

The subtle one. The scanner does **not** try to match keywords against the
input. It scans a maximal run of alphanumerics, and only then asks a map whether
that exact text is reserved:

```go
text := s.Source[s.start:s.current]
t, ok := keywords[text]
if !ok { t = token.IDENTIFIER }
```

Do it the other way — check "does the input start with `or`?" — and `orchid`
lexes as `or` followed by `chid`. Verified:

```
orchid or o  →  IDENTIFIER(orchid), OR(or), IDENTIFIER(o)
```

This is maximal munch again, and it is the reason keywords are *reserved*: once
`or` is in that map, no variable can be called `or`, because the scanner has no
way to know you meant it as a name.

## The scanner never fails

`ScanToken` has no error return. On a character it cannot start a token with, it
calls `errors.Error(line, "Unexpected character.")` and keeps going — so one bad
character costs one message, not the rest of the file:

```
@#$  →  three errors, then EOF
```

Reporting happens through the package-global `errors.HadError` in
[`pkg/errors`](../pkg/errors/errors.go). `main.go` checks that flag *after*
lexing and refuses to parse when it is set. So the contract is: the scanner
always returns a token slice, always EOF-terminated, and separately raises a
flag saying whether that slice is trustworthy.

That global is the ugliest thing in the front end, and it is the book's design.
It is why scanner and parser tests cannot use `t.Parallel()`.

## What the scanner refuses to know

The list is as instructive as what it does:

- **No nesting.** `(` and `)` are just tokens. Whether they balance is the
  parser's problem.
- **No context.** `-` scans identically in `1 - 2` and `-x`. Whether it is
  binary or unary is decided by grammar position.
- **No declarations.** `foo` is `IDENTIFIER(foo)` whether or not it exists.
- **No types.** Every number is `float64`, no matter how it was written.

Each of these is a thing the scanner *could* track and deliberately does not,
because tracking it would require the unbounded memory that splitting the passes
was meant to avoid.

## Known edges

Measured, not inferred. Reproduce with `-tokens`.

| input | result | why |
|---|---|---|
| `1.` | `NUMBER(1)`, `DOT` | `number()` only eats the `.` if a digit follows it |
| `.5` | `DOT`, `NUMBER(5)` | Lox has no leading-dot literals |
| `"café"` | one `STRING`, intact | string bodies are copied as bytes, so UTF-8 passes through |
| `var café` | **2 errors**, identifier truncated to `caf` | the scanner is byte-oriented: `IsAlpha` rejects each byte of `é` |
| a 400-digit number | error, no token | overflows `float64`; see below |

The last one used to be a wart worth studying. `number()` calls
`strconv.ParseFloat`, and on failure it returned without adding a token *and
without reporting anything*. The parser then saw a gap in the stream and blamed
whatever came next:

```lox
print 99999…9;        // 400 digits
[line 1] Error at ';': Expect expression.        // ← the old message
[line 1] Error: Number literal is too large.     // ← now
```

Nothing ever ran wrongly, because the program was refused either way. But the
message pointed at the semicolon when the problem was the literal, which is the
kind of error that costs an afternoon. Reporting also means `main.go`'s
`errors.HadError` check fires *before* parsing, so the misleading second message
is gone rather than merely outranked.

Two things about that check are worth knowing:

- **Overflow is the only reachable failure.** The text handed to `ParseFloat` is
  digits with at most one embedded dot, so it is always syntactically valid, and
  Go signals underflow by quietly returning zero rather than an error. A
  400-zero decimal like `0.000…1` scans fine and is `0`.
- **jlox disagrees.** Java's `Double.parseDouble` returns `Infinity` for an
  overflowing literal without complaining, so the book's implementation accepts
  it. Refusing is the better answer here: the source named a specific finite
  number, and substituting `Infinity` is a wrong value rather than an imprecise
  one. Lox can still *produce* an infinity at run time — `print 1/0;` gives
  `+Inf` — which is a different thing from a literal that quietly stopped
  meaning what it says.

The non-ASCII identifier case, by contrast, is a deliberate limitation rather
than a bug: Lox identifiers are ASCII by definition. Supporting more means
decoding runes with `utf8.DecodeRuneInString` instead of indexing bytes, which
changes `advance`, `peek`, and `peekNext` together.

---

# Part 2 — The parser

Files: [`parser/parser.go`](../parser/parser.go) (recursive descent),
[`parser/llk/`](../parser/llk/) (table-driven),
[`parser/algorithm.go`](../parser/algorithm.go) (the interface over both),
[`ast/`](../ast/) (the output).

## A grammar is a set of rewrite rules

The canonical copy lives in [`grammar.md`](grammar.md), chapter by chapter. A
rule names a construct and says what it is made of:

```
term    → factor ( ( "-" | "+" ) factor )* ;
factor  → unary ( ( "/" | "*" ) unary )* ;
```

Uppercase and quoted things are terminals — token types, straight from the
lexer. Lowercase things are nonterminals, and most of them become an AST node.

## Precedence is the shape of the rule stack

The single most important idea in the parser, and it is not a mechanism — it is
a *layout*. Each rule calls the rule one rung tighter than itself:

```
expression → assignment → logic_or → logic_and → equality
           → comparison → term → factor → unary → call → primary
```

`term` handles `+`/`-` and calls `factor`, which handles `*`//`/`. Because
`factor` is called *inside* `term`, a multiplication is always finished before
the surrounding addition can see it. That is all precedence is here:

```sh
$ go run . -print=ast   # print 1 + 2 * 3;
(print (+ 1 (* 2 3)))
```

Nothing compares precedence numbers at run time. The answer was decided by which
function calls which.

## Associativity: loop or recurse

Same rung, two operators — `1 - 2 - 3`. Which grouping you get depends on
whether the rule loops or recurses:

```go
func (p *Parser) term() (ast.Expr, error) {
    expr, _ := p.factor()                      // first operand
    for p.match(token.MINUS, token.PLUS) {     // a LOOP
        operator := p.previous()
        right, _ := p.factor()
        expr = &ast.Binary{Left: expr, Operator: operator, Right: right}
    }                                          // ↑ folds into the LEFT
    return expr, nil
}
```

Each turn wraps what it already has as the *left* child, so it nests leftward:

```
1 - 2 - 3   →   (- (- 1 2) 3)      left-associative — correct for arithmetic
```

Assignment is right-associative, and gets it by recursing instead of looping —
`assignment()` calls itself for the right-hand side:

```
a = b = 3   →   (= a (= b 3))
```

Loop for left, recurse for right. That is the entire rule.

## One token of hindsight

Assignment has a problem: by the time you see `=`, you have already parsed the
left side, and you do not know whether it was a valid target until you look at
what it turned out to be. The fix is not backtracking:

```go
expr, _ := p.or()                 // parse the left side as an ordinary expression
if p.match(token.EQUAL) {
    value, _ := p.assignment()
    switch target := expr.(type) {
    case *ast.Variable: return &ast.Assign{Name: target.Name, Value: value}, nil
    case *ast.Get:      return &ast.Set{Object: target.Object, …}, nil
    }
    return nil, errorAt(equals, "Invalid assignment target.")
}
```

Parse first, decide from the node you got. This is why `a.b = 1` needed no extra
lookahead when chapter 12 added properties — only one more `case`.

## Error recovery

A parser that stops at the first error is useless for learning. `synchronize()`
discards tokens until it reaches a plausible restart point — just past a `;`, or
just before a keyword that can only begin a statement:

```lox
var a = ;      // error
print 1;       // still parsed
var b = ;      // second, independent error
print 2;
```

```
[line 1] Error at ';': Expect expression.
[line 3] Error at ';': Expect expression.
```

Two messages, not one, and not fifty. The parser collects errors and refuses to
run the program; it does not try to guess what you meant.

## Two algorithms, one interface

This repo has two parsers producing identical trees. They meet at
[`parser/algorithm.go`](../parser/algorithm.go):

```go
type Algorithm interface {
    Parse() (ast.Expr, error)
    ParseAll() ([]ast.Expr, []error)
    ParseProgram() ([]ast.Stmt, []error)
}
```

| | `parser` | `parser/llk` |
|---|---|---|
| decisions | in code, one function per rule | in a table, built before parsing |
| recursion | the Go call stack | an explicit stack |
| grammar | implied by the code | data you can print |
| ambiguity | silently resolved by branch order | refuses to build, names both rules |
| lookahead | whatever each rule peeks at | exactly *k*, uniformly |

The second column's advantage is the one worth internalising: a hand-written
parser cannot tell you your grammar is ambiguous. It just picks whichever branch
you wrote first and parses a language subtly different from the one you meant.
The table is checked, so it *can*. [`llk-parser.md`](llk-parser.md) is the deep
dive.

Everything downstream — resolver, interpreter — sees only trees, so it cannot
tell which parser ran. `parser/algorithm_test.go` asserts they agree.

---

# Part 3 — The contract between them

Three promises hold the two halves together.

1. **The token slice always ends in EOF.** The lexer always appends one. Both
   parsers *also* append one if handed a slice without it, because they accept
   token slices from tests too. Every `isAtEnd` is a check for that sentinel
   rather than a bounds check, so a missing EOF would run off the end.

2. **`Literal` is nil except for NUMBER and STRING.** The interpreter uses `nil`
   for Lox `nil`, so this cannot be approximate.

3. **Errors travel through a package global, not a return value.** Both passes
   report and continue; `main.go` checks `errors.HadError` between stages and
   stops. The exit codes come from that: `65` for a syntax or static error, `70`
   for a runtime one.

---

# A reading path

If you want to walk the front end once, in an order that builds:

1. [`lexer/token/token.go`](../lexer/token/token.go) — the alphabet. Small.
2. [`lexer/scanner/scanner.go`](../lexer/scanner/scanner.go) — `scanToken`
   first, then `number`, `identifier`, `stringLiteral`.
3. [`grammar.md` § Chapter 6](grammar.md#chapter-6--expressions-precedence-ladder)
   — the precedence ladder as rules.
4. [`parser/parser.go`](../parser/parser.go) — `expression` down to `primary`,
   in that order. Notice each function calls the next.
5. [`ast/expr.go`](../ast/expr.go) and [`ast/printer.go`](../ast/printer.go) —
   what gets built, and the visitor that renders it.
6. [`parser/parser.go`](../parser/parser.go) again — `synchronize`,
   `declaration`, `statement`. The statement layer, once expressions make sense.
7. [`llk-parser.md`](llk-parser.md) — the second algorithm, once the first is
   comfortable.

The per-chapter docs pick up from there:
[05](05-representing-code.md) · [07](07-evaluating-expressions.md) ·
[08](08-statements-and-state.md) · [09](09-control-flow.md) ·
[10](10-functions.md) · [11](11-resolving-and-binding.md) ·
[12](12-classes.md) · [13](13-inheritance.md)

# Exercises

Cheap ones, in rough order of difficulty. Predict first, then check.

1. Dump the tokens for `var average = (1 + 2) / 2;` and say which two carry a
   non-nil `Literal`, and why the others must not.
2. Predict the tree for `1 + 2 * 3 - 4`, then check with `-print=ast`. Then
   predict `-print=rpn` for the same input, and say what RPN is able to drop.
3. In `parser/parser.go`, change `term()`'s `for` to an `if`. Which test fails,
   and what does the tree become?
4. Swap which operators the two rungs handle: give `term()` the `*` and `/`
   cases and `factor()` the `+` and `-` ones, leaving the call structure alone.
   Predict what `1 + 2 * 3` parses to before running it, and say which of
   "precedence" and "associativity" you just changed.
5. Delete `"or"` from the scanner's `keywords` map. What does `1 or 2` do now,
   and at which stage does it fail?
6. Add a `%` operator: token type, scanner case, grammar rung, parser function,
   interpreter case. Which of the five is the one you would forget?
7. This repo refuses a literal too large for a `float64`; jlox accepts it and
   gets `Infinity`, because Java's `Double.parseDouble` does not complain. Both
   are defensible. Write the argument for jlox's side, then check whether
   `print 1/0;` changes your mind. (The scanner test is
   `TestScanTokensNumberTooLarge`.)
8. Make the unterminated-string error report the line the string *opened* on
   rather than where the file ended. Verified today it says line 4 for a string
   opened on line 2.
