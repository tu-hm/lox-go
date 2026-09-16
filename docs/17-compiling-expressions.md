# Chapter 17 — Compiling expressions

Implementation of
[Compiling Expressions](https://craftinginterpreters.com/compiling-expressions.html).
This is where the two halves meet: the scanner from chapter 16 feeds a Pratt
parser, the parser emits bytecode, and the VM from chapter 15 runs it. `clox`
evaluates a program for the first time.

> Status: implemented, including challenges 1 and 3 as code and challenge 2 as
> prose. `clox somefile.lox` compiles one expression and prints its value;
> `clox` with no argument is a REPL that does the same per line. Exit code 65
> has a producer. `?:` parses and reports that it cannot be compiled, because
> choosing between two operands needs a jump and jumps arrive in chapter 23.

For a guided walkthrough, follow the [Chapter 17 learning plan](17-learning-plan.md).

## What the chapter adds

| Type | Shape | What it is |
|---|---|---|
| `Parser` | `current`, `previous`, `hadError`, `panicMode` | the entire parser state: two tokens and two flags |
| `Precedence` | 12 values | binding power, as a number you can compare |
| `ParseRule` | `prefix`, `infix`, `precedence` | one row per token type; 43 rows, 7 of them non-empty |

| File | What it does |
|---|---|
| `compiler.c` | tokens in, bytecode out — `parsePrecedence`, the rules table, five emit helpers |
| `vm.c` | `interpret` compiles into a chunk and hands it to `interpretChunk` |
| `main.c` | `-tokens`, `-dump` and `-trace`; the exit codes stop being decoration |
| `test/test_compile.c` | the golden driver: 26 expressions, compiled, disassembled and run |
| `tool/rpndiff.sh` | the emitted bytecode against the Go AST printer, expression by expression |

## Two tokens is the whole parser state

Chapter 16's claim was that scanning on demand costs nothing. This is the chapter
where it is paid for, and the bill is that the parser may never look further back
than one token.

[`parser/parser.go`](../parser/parser.go) has a `Tokens []token.Token` and a
`Current int`. It can look anywhere. It uses that exactly once, and usefully:
assignment is parsed by parsing the left side as an ordinary expression and then,
on seeing `=`, deciding after the fact whether what it built was a valid target
(`parser.go:481-491`). That is one token of hindsight over a tree, and it is only
possible because the tree is still there.

`Parser` in [`clox/compiler.c`](../clox/compiler.c) is `previous`, `current`, and
two booleans. There is nothing to look back at. Chapter 21 solves the same
assignment problem with a `bool canAssign` threaded through every parse function
— information pushed *forward* because it cannot be recovered backward.

The constraint is what makes Pratt parsing the natural algorithm here rather than
one option among several. An algorithm that decides everything from the token in
front of it and a number is exactly an algorithm that needs no history.

## Precedence as data

There are now three encodings of the same precedence ladder in this repository,
and they differ in where the answer physically lives.

| | Where precedence lives | Cost of `1` |
|---|---|---|
| [`parser/parser.go`](../parser/parser.go) | which function calls which | 11 stack frames |
| [`parser/llk/grammar.go`](../parser/llk/grammar.go) | which nonterminal derives which | a table lookup per rung |
| [`clox/compiler.c`](../clox/compiler.c) | a number in a table | 1 comparison |

[`docs/front-end.md`](front-end.md#precedence-is-the-shape-of-the-rule-stack)
puts the Go version flatly: *"Nothing compares precedence numbers at run time.
The answer was decided by which function calls which."* Parsing the expression
`1` there means calling `expression`, `assignment`, `or`, `and`, `equality`,
`comparison`, `term`, `factor`, `unary`, `call` and `primary` — eleven frames to
discover that none of the ten operators is present.

`parsePrecedence` does it with one comparison:

```c
while (precedence <= getRule(parser.current.type)->precedence) {
```

That is the only precedence test in the parser. Everything else — which function
handles `-`, whether `-` is even legal here — is a table lookup.

The table is also where the chapter's growth shows. Of 43 rows, 7 have anything
in them. `and`, `or`, `!`, the comparison operators, `true`/`false`/`nil`, string
literals, identifiers, `(` as a call, `.` as a property get: each is a chapter,
and each lands as a *row*, not as a new rung in a ladder that every other rung
has to be re-read to understand.

## `+ 1` is associativity

`binary()` parses its right operand one precedence level tighter than itself:

```c
ParseRule* rule = getRule(operatorType);
parsePrecedence((Precedence)(rule->precedence + 1));
```

Passing `rule->precedence` instead would let the right operand swallow the next
operator at the same level, and `1 - 2 - 3` would parse as `1 - (2 - 3)`. Both
versions compile, both run, and the difference is one character.

It is checkable in two places. The golden file has `1 - 2 - 3` and `12 / 3 / 2`
in it, whose values are `-4` and `2` when this is right and `2` and `8` when it
is not; and `tool/rpndiff.sh` disagrees with the Go parser on 10 of its 30
expressions. The Go parser says the same thing with control flow instead of a
number — a `for` loop folds the left operand in, recursion builds to the right —
which [`front-end.md`](front-end.md#associativity-loop-or-recurse) compresses to
*"Loop for left, recurse for right. That is the entire rule."* Here the rule is
`+ 1`.

The other precedence argument in the file is `unary()`'s:

```c
parsePrecedence(PREC_UNARY);
```

not `expression()`. Unary binds tighter than every binary operator, so its
operand has to stop at the first thing that binds looser: `-1 + 2` is `(-1) + 2`,
not `-(1 + 2)`. That one is worth 6 of the 30 differential expressions.

## An error is a token, and `advance` is where that is paid

Chapter 16 chose to put scanner errors *in* the token stream rather than in a
global, and said the bill would come due here. It does, in two places, and it is
smaller than the design discussion around it.

`advance` loops instead of returning:

```c
for (;;) {
  parser.current = scanToken();
  if (parser.current.type != TOKEN_ERROR) break;

  errorAtCurrent(parser.current.start);
}
```

The parser never sees a `TOKEN_ERROR`. It is consumed and converted here, which
is the point of putting it in the stream: there is no second thing a caller could
forget to check.

And `errorAt` needs one branch that the Go reporter does not:

```c
} else if (token->type == TOKEN_ERROR) {
  // Nothing. The message is the token.
}
```

A `TOKEN_ERROR`'s `start` points at a string literal inside the scanner, not into
the source, so the usual `at '%.*s'` would print the message twice. That is the
whole cost. [`pkg/errors.ErrorToken`](../pkg/errors/errors.go) has no equivalent
case because a Go scanner error never becomes a token at all.

The interesting flag is the other one. `panicMode` exists only to stop one
mistake from printing five messages, and it does nothing else — in particular it
does not stop parsing. Deleting the two lines that implement it does not change
whether anything compiles; it changes `1 + + 2` from one message to two, `@` from
one to two, and `1 ? 2` from one to three. The parser was always going to keep
going. Suppressing the *message* is not the same as suppressing the *recovery*,
and chapter 17 has only the first half because it has no statement boundary to
recover to.

## The chunk the compiler writes is the chunk chapter 14 designed

`emitConstant` does not exist in the book's shape here:

```c
static void emitConstant(Value value) {
  if (!writeConstant(currentChunk(), value, parser.previous.line)) {
    error("Too many constants in one chunk.");
  }
}
```

The book emits `OP_CONSTANT` and a one-byte index, and refuses at 256 constants.
[`writeConstant`](../clox/chunk.c) is chapter 14's challenge 2 and picks the
instruction: `OP_CONSTANT` below index 256, `OP_CONSTANT_LONG` with a three-byte
operand above it. This chapter is the first time anything but a hand-written
demo asks for that choice, and the first time the compiler and the VM have to
agree on the encoding without a human holding both ends.

Two consequences, both visible in
[`clox/test/expected-compile.txt`](../clox/test/expected-compile.txt), which
compiles `0 + 1 + ... + 299` and prints the instructions either side of the
transition:

```
0764    | OP_CONSTANT       255 '255'
0766    | OP_ADD
0767    | OP_CONSTANT_LONG  256 '256'
-- 988 code bytes, 300 constants
44850
```

The window shows the compiler choosing; the sum shows the VM decoding what it
chose. A three-byte operand read in the wrong byte order would still run and
would load some other constant, so the value is the check and the disassembly is
only the explanation.

And `test/limit/too_many_constants.lox` — which
[`tool/booktest.py`](../tool/booktest.py) skips with *"Limits of clox's bytecode
format"* — is not a limit this clox has. Ours is 2²⁴.

`writeConstant` also lost its `exit(1)`. Chapter 14 left a comment saying *"there
is nowhere to report it to… Chapter 17 gets both, and this becomes a compile
error"*; it now returns `false` and the compiler reports it like any other
source error.

One thing the book has that is missing here: `emitBytes`. Its only caller was
`emitBytes(OP_CONSTANT, makeConstant(value))`, and `writeConstant` took that job,
so under `-Werror` it could not stay. It comes back in chapter 21, where every
instruction with an operand is emitted by hand.

## The dump goes to stderr, and the three flags exist because of it

`DEBUG_PRINT_CODE` in the book disassembles the finished chunk to stdout. This
build asserts that `clox`'s stdout is byte for byte identical across six
configurations, so it goes to stderr, behind the same runtime switch
`DEBUG_TRACE_EXECUTION` got in chapter 15:

```c
#ifdef DEBUG_PRINT_CODE
  if (!parser.hadError && codeTrace != NULL) {
    disassembleChunk(codeTrace, currentChunk(), "code");
  }
#endif
```

`codeTrace` is `NULL` unless `clox -trace` set it. `!parser.hadError` is not
decoration: a chunk that failed to compile has holes in it, and disassembling one
prints whichever bytes happened to land beside each other.

That leaves `clox` with three flags, deliberately spelled like
[`glox`](../main.go)'s, because two of them exist to be driven by tools that read
both binaries:

| Flag | What it does | Who reads it |
|---|---|---|
| `-tokens` | chapter 16's token dump, on stdout | `make test`, [`tool/scandiff.sh`](../tool/scandiff.sh) |
| `-dump` | compile and disassemble, do not run, on stdout | [`tool/rpndiff.sh`](../tool/rpndiff.sh) |
| `-trace` | the code dump and the execution trace, on stderr | people |

`-tokens` is where the chapter-16 golden went. The scope boundary there predicted
the token dump would have to "move behind a flag or into a test binary"; a flag
won, because `tool/scandiff.sh` needs it in the shipped binary and not in a test
one.

The disassembler now has three callers with three different streams — `-dump` to
stdout because it is the program's output, `-trace` to stderr because it is
diagnostics, and `test_compile` to stdout because there it is the test's subject.
That is the `FILE* out` parameter chapter 15 added earning its keep.

## Challenge 1 — the parser, traced

The challenge asks for a written trace of `(-1 + 2) * 3 - -4`: which functions
are called, in what order, by whom, with what arguments. Writing one by hand is
the exercise. This is how the answer gets checked rather than believed —
`make parsetrace` builds `clox` with `CLOX_TRACE_PARSER` defined, which prints
every entry to and return from the parse functions, indented by depth:

```
source: (-1 + 2) * 3 - -4
parsePrecedence(ASSIGNMENT) previous=<none> current='('
  grouping() previous='(' current='-'
    parsePrecedence(ASSIGNMENT) previous='(' current='-'
      unary(MINUS) previous='-' current='1'
        parsePrecedence(UNARY) previous='-' current='1'
          number() previous='1' current='+'
          number returns
        parsePrecedence returns
      unary returns
      binary(PLUS) previous='+' current='2'
        parsePrecedence(FACTOR) previous='+' current='2'
          number() previous='2' current=')'
          number returns
        parsePrecedence returns
      binary returns
    parsePrecedence returns
  grouping returns
  binary(STAR) previous='*' current='3'
    parsePrecedence(UNARY) previous='*' current='3'
      number() previous='3' current='-'
      number returns
    parsePrecedence returns
  binary returns
  binary(MINUS) previous='-' current='-'
    parsePrecedence(FACTOR) previous='-' current='-'
      unary(MINUS) previous='-' current='4'
        parsePrecedence(UNARY) previous='-' current='4'
          number() previous='4' current=''
          number returns
        parsePrecedence returns
      unary returns
    parsePrecedence returns
  binary returns
parsePrecedence returns
7
```

Four things in there are worth reading twice.

**The argument to `parsePrecedence` is never the operator's own precedence.**
`binary(PLUS)` asks for `FACTOR`, one above `TERM`. `binary(STAR)` asks for
`UNARY`, one above `FACTOR`. That is the `+ 1`.

**`binary(PLUS)` and `binary(STAR)` are at different depths.** `+` is called from
inside `grouping`'s `parsePrecedence`; `*` is called from the outermost one. The
parentheses are gone from the output because they were never a node — `grouping`
emits nothing at all, and by the time `*` is looked up, the `(...)` is just a
value on the stack.

**`binary(MINUS)` and `unary(MINUS)` are the same token type**, three lines
apart, dispatched to different functions. Nothing about the token distinguishes
them. The parser was in a different place, and that is the entire difference —
which is challenge 2.

**The last `binary(MINUS)` has `current='-'`.** The parser has just decided `-`
is subtraction, and the next token is also `-`, which will be negation. One
lookahead token, two opposite meanings, decided by position.

The trace costs nothing in an ordinary build: `TRACE_IN` and `TRACE_OUT` expand
to `((void)0)` and the formatting functions are inside the `#ifdef`.

## Challenge 2 — prefix and infix in one row

In full Lox, exactly two tokens have both columns of the rules table filled.

`-` is the one the chapter names: negation in prefix position, subtraction in
infix position. The other is `(` — grouping when it starts an expression, a call
when it follows one. `[TOKEN_LEFT_PAREN] = {grouping, call, PREC_CALL}` is the
final shape of that row in chapter 24, and it is the more interesting of the two,
because the Go parser cannot state it in one place at all: grouping lives in
`primary` (`parser.go:682`) and calls live in `call` (`parser.go:630`), two rungs
apart, and nothing in that file says they are the same character.

Nothing else in Lox comes close. `!` is prefix only, `.` and the comparison
operators are infix only, `super` and `this` are prefix only.

C has more, and they are why C is hard to parse:

| Token | Prefix | Infix |
|---|---|---|
| `-` | negate | subtract |
| `+` | unary plus | add |
| `*` | dereference | multiply |
| `&` | address-of | bitwise and |
| `(` | grouping, **or a cast** | call |

`*` and `&` are the famous ones, because their two meanings are not even the same
*kind* of thing — `*p` is a value and `a * b` is arithmetic, and `*` also appears
in declarations as a type constructor. `(` is worse still: `(x)*y` is a
multiplication if `x` is a variable and a cast-then-dereference if `x` is a type
name, which is the lexer-feedback problem, and no amount of precedence table
fixes it.

`++` and `--` look like they belong on the list and do not. They are prefix and
*postfix*, which is a third position a Pratt parser handles as an infix rule with
no right operand — the table has a slot for it, which is more than the ladder
does.

## Challenge 3 — a mixfix operator

Implemented as far as it can be. `?` and `:` are now real tokens
([`clox/scanner.h`](../clox/scanner.h)), and `ternary` is one infix rule:

```c
static void ternary(void) {
  Token operatorToken = parser.previous;

  expression();
  consume(TOKEN_COLON, "Expect ':' after then branch of conditional.");
  parsePrecedence(PREC_CONDITIONAL);

  errorAt(&operatorToken,
          "The '?:' operator parses but cannot be compiled yet.");
}
```

The challenge is "show how you would hook it up to the parser and handle the
operands", and the answer is that a mixfix operator needs no new machinery at
all. It is an infix rule that happens to consume a delimiter in the middle. The
two precedences in those three lines are the entire content:

**The middle operand parses at the loosest precedence there is.** `?` and `:`
bracket it exactly the way parentheses would, so nothing inside can escape: once
assignment exists in chapter 21, `a ? b = 1 : c` will be legal for the same
reason `(b = 1)` is. `expression()`, not `parsePrecedence(PREC_CONDITIONAL)`.

**The else branch parses at `PREC_CONDITIONAL`, not one tighter.** That missing
`+ 1` is what makes `?:` right-associative: `a ? b : c ? d : e` groups as
`a ? b : (c ? d : e)`. It is the same one-character decision as `binary()`'s,
made the other way, and it is why the two are worth reading side by side.

`PREC_CONDITIONAL` itself is a new rung between `PREC_ASSIGNMENT` and `PREC_OR`,
which is where C puts the conditional operator.

**No bytecode, and that is not a shortcut.** Choosing between two operands means
*not evaluating* one of them, which needs a jump, and jumps arrive in chapter 23.
The book says no bytecode is required; emitting all three operands and hoping
would compile, run, and produce the wrong answer, which is worse than emitting
none. So the operator parses correctly and then reports that it cannot be
compiled, at the `?` rather than wherever the parse happened to end:

```
-- 1 ? 2 * 3 : 4
[line 1] Error at '?': The '?:' operator parses but cannot be compiled yet.
-- 1 ? 2
[line 1] Error at end: Expect ':' after then branch of conditional.
```

One message each, and neither is the one a confused parser would give. The first
is reported at the `?`, which means the whole of `2 * 3 : 4` was consumed by
`ternary` — `make -C clox parsetrace PARSE_TRACE_EXPR='1 ? 2 * 3 : 4'` shows the
shape. The second shows the missing-colon error arriving *before* the
not-implemented one, because the parse runs to completion first and `panicMode`
drops whatever comes second.

The Go implementation has neither token and never took the chapter-6 ternary
challenge, so this is the one place the two token vocabularies differ. It costs
nothing: no file in the corpus contains a `?`, and the only `:` outside a comment
is inside a string literal in `test/benchmark`, which
[`tool/scandiff.sh`](../tool/scandiff.sh) does not walk.

## Verification

```sh
make -C clox test                      # five golden diffs + two CLI checks
make -C clox test HEAP=own
make -C clox test MODE=release
make -C clox test MODE=release HEAP=own
make -C clox asan
make -C clox asan HEAP=own
tool/scandiff.sh                       # both scanners, the whole corpus
tool/rpndiff.sh                        # both parsers, every expression
```

`make test` pins five files and runs two checks. Two of the files are inherited
from chapter 15 unchanged. Three are this chapter's:

- [`expected-compile.txt`](../clox/test/expected-compile.txt) — 26 expressions,
  each disassembled and run. Precedence, both associativities, unary chains,
  line attribution, and the `OP_CONSTANT_LONG` transition.
- [`expected-compile-err.txt`](../clox/test/expected-compile-err.txt) — one
  message per bad expression, which is what `panicMode` is for.
- [`expected-tokens.txt`](../clox/test/expected-tokens.txt) — chapter 16's, now
  produced by `clox -tokens`.

The two checks are the ones a golden file cannot make. `clox` must exit 65 on a
file that does not compile — until this chapter nothing could produce that code.
And:

```sh
clox ../test/expressions/evaluate.lox    # prints 2
```

That is the book's own corpus file. It holds `(5 - (3 - 1)) + -1` and
`// expect: 2`, it has been sitting in `test/expressions/` unused since the
corpus was vendored, and it is the first file in it clox has ever been able to
run. [`tool/booktest.py`](../tool/booktest.py) skips that directory with *"No
interpreter yet in those chapters"*; for `clox`, this is that chapter.

**`tool/rpndiff.sh` is the check the golden files cannot be**, and it is chapter
17's answer to what `scandiff.sh` did for chapter 16. The observation it rests on
is that bytecode for an expression *is* its postfix traversal — operands are
pushed before the operator that consumes them, which is the definition of RPN.
So this:

```
OP_CONSTANT 1, OP_NEGATE, OP_CONSTANT 2, OP_ADD, OP_CONSTANT 3, OP_MULTIPLY, ...
```

and the string [`ast.RPNPrinter`](../ast/rpn.go) produces for the same source:

```
1 negate 2 + 3 * 4 negate -
```

are the same sentence. Parentheses leave no trace in either — `grouping()` emits
nothing, and `VisitGroupingExpr` drops them — which is not a coincidence so much
as the same fact stated twice.

```
30 compared, 0 disagreed
```

Thirty expressions, each parsed by a table-driven Pratt parser in C and by an
eleven-function recursive-descent ladder in Go written months earlier, and
required to produce the same operand order. It tests exactly the two things a new
precedence parser gets wrong, because precedence and associativity are the only
things that change that order. Parsing the right operand of a binary operator at
the wrong level fails 10 of the 30; parsing a unary operand at the wrong level
fails 6.

Its limits are in [`tool/expressions.txt`](../tool/expressions.txt) rather than
hidden: numbers only, arithmetic only — that is all chapter 17's clox has — and
nothing past six significant digits, because the C disassembler prints constants
with `%g` and the Go printer does not round at all.

The Go implementation is untouched and must stay green:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
./tool/booktest.sh
```

## Scope boundary

Challenges 1 and 3 are code; 2 is prose, because it has none in it.

All four of chapter 16's predictions held. `compile` grew a `Chunk*` and a
`bool`; the token dump moved, to a flag rather than a test binary, because
`scandiff.sh` needs it in the shipped binary; `TOKEN_ERROR` got its first
consumer in `advance` and cost `errorAt` exactly one branch; and `strtod`
arrived, which is what made `rpndiff.sh` possible — the first comparison between
the two implementations on something other than token types.

Three things here are built for chapters that have not arrived.
`PREC_CONDITIONAL` and `ternary` parse an operator nothing can compile until
chapter 23 has jumps. `INTERPRET_RUNTIME_ERROR` is still unreachable: every
`Value` is a `double`, every operator is defined on every pair of them, and the
VM's only failure mode is an opcode it does not recognise. And `panicMode` is
half a mechanism — it suppresses duplicate messages but has nothing to
resynchronise *to*, because there are no statement boundaries.

Chapter 18 adds types of values, and the pressure lands in three places. `Value`
stops being a `double`, so the `BINARY_OP` macro has to unwrap and re-wrap its
operands and check they are numbers — which is where `INTERPRET_RUNTIME_ERROR`
finally gets a producer, and where `getLine`'s linear walk moves onto the error
path for real. `fprintValue` stops being one `printf`, and the moment it does,
`tool/rpndiff.sh`'s `%g` caveat becomes a question about `nil` and `true` instead
of about rounding. And the rules table fills in: `true`, `false`, `nil` and `!`
are four rows and one new parse function, which is the first test of the claim
that adding syntax to this parser is additive.
