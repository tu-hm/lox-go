# Chapter 16 — Scanning on demand

Implementation of
[Scanning on Demand](https://craftinginterpreters.com/scanning-on-demand.html).
This is where clox reads source text for the first time: a scanner, a compiler
that does nothing with it yet, and a `main` that is a program rather than a demo.

> Status: implemented, including challenge 1 as code and challenges 2 and 3 as
> prose. `clox` is now a REPL and a file runner. `compile()` prints the token
> stream and produces no bytecode — chapter 17 is where it emits instructions.
> String interpolation is scanned, including nested, and nothing consumes those
> tokens yet.

For a guided walkthrough, follow the [Chapter 16 learning plan](16-learning-plan.md).

## What the chapter adds

| Type | Shape | What it is |
|---|---|---|
| `TokenType` | 42 values | every terminal in the grammar, plus `ERROR` and `EOF` |
| `Token` | `type`, `start`, `length`, `line` | 24 bytes, owns nothing |
| `Scanner` | `start`, `current`, `line`, `braces`, `interpolation` | the whole scanner's state |

| File | What it does |
|---|---|
| `scanner.c` | text in, one token out, on request |
| `compiler.c` | the scanner's first consumer; prints tokens and returns |
| `main.c` | `repl` and `runFile`, replacing chapter 14's demo chunk |
| `test/test_chunk.c` | that demo chunk, moved so its golden output survives |

## On demand is about memory, and it is a smaller number than it sounds

The chapter's title is its one architectural claim: jlox scans the whole file
into a `List<Token>` before the parser starts, and clox scans a token when the
compiler asks for one and never holds more than two.

The two implementations in this repository make the comparison exact.
`sizeof(Token)` in C is 24 bytes — a 4-byte enum, a pointer, two ints. The Go
`token.Token` is 56: a `TokenType string` header is 16 bytes, `Lexeme string` is
another 16, `Literal any` is an interface header at 16, and `Line int` is 8. And
the Go scanner keeps all of them: [`lexer.Lex`](../lexer/lexer.go) returns a
slice, and [`main.go`](../main.go) holds it until the parser is done.

The biggest file in the test corpus is 521 tokens. That is 29 KB of Go tokens
against 24 bytes of C, and both numbers are irrelevant — no real program is
large enough for either to matter, and the book says as much. What the design
actually buys is elsewhere:

- **The scanner has no output type.** There is no `TokenList`, no growth
  strategy, no ownership question about who frees it. That is a data structure
  and an allocation the C implementation never has to write.
- **The compiler's state is two tokens.** `previous` and `current`, in chapter
  17. A single-pass compiler that cannot look further back than one token is a
  strong constraint, and it is what makes Pratt parsing the natural choice
  rather than one option among several.
- **Errors arrive in order.** A scanner that runs to completion first reports
  every lexical error before any syntax error, no matter where they were in the
  file. Interleaved is what a user expects.

The third one is worth a moment, because the Go implementation is the
counterexample and it is a real difference in behaviour, not a theoretical one.
Its output for one typo is two messages in file order only because
[`main.go`](../main.go) deliberately parses past the scanner error; that took a
bug fix to get right, and it is still two independent passes agreeing to look
like one.

## A token owns nothing

`Token` is a pointer and a length into the buffer the caller passed to
`initScanner`. There is no copy of the text and there is no parsed value.

Two things follow. The source buffer has to outlive every token taken from it,
which is why [`main.c`](../clox/main.c)'s `runFile` frees it only after
`interpret` returns — and it is the whole reason that function reads the file
into one allocation rather than streaming it.

And a number is still text. The Go scanner calls `strconv.ParseFloat` during
scanning and hangs a `float64` on the token, which is how it is able to report
`Number literal is too large.` at scan time — see
[Known edges](front-end.md#known-edges). clox has nowhere to put the answer and
nothing to report it with, so `123` stays three characters until chapter 17's
compiler calls `strtod`. The scanner's job is to say where a token is, not what
it means.

## An error is a token

This is the chapter's real design decision, and the two implementations in this
repository land on opposite sides of it.

The Go scanner has no error return. It calls `errors.Error(...)`, sets the
package-global `errors.HadError`, and keeps going; the caller gets a token slice
and, separately, a flag telling it whether to trust it. clox returns a
`TOKEN_ERROR` whose `start` points at a message literal rather than into the
source — the error is *in the stream*, in position.

The trade is about who can forget. Two values means a caller can read one and not
the other, compile, run, and be wrong. One value means the only way to ignore the
error is to ignore a token, which a parser structurally cannot do.

What the global buys is uniformity: a scanner error and a parser error go through
one reporting path and come out looking identical. clox pays for its version in
chapter 17, where `errorAt` has to special-case `TOKEN_ERROR` — it cannot print
`at '%.*s'` for a token whose `start` is not in the source buffer.

Both are the same book. The difference is one chapter of distance and a language
without globals worth having.

## The keyword trie, and what it is actually beating

`identifierType` is a hand-written trie: switch on the first character, then on
the second where a language has two keywords sharing one. The Go scanner does the
same job with a `map[string]TokenType`.

The trie wins, and not for the reason it looks like. A map lookup hashes the
whole identifier — every byte of `someVeryLongVariableName` — and then compares
it against a bucket. The trie rejects on the first byte, and the first byte of
almost every identifier in almost every program is not one of the thirteen that
start a Lox keyword. It fails without touching memory beyond the one byte it
already had.

What both share, and what actually matters, is the order of operations: scan a
maximal run of identifier characters *first*, then ask whether the text is
reserved. Ask the other way round — "does the input start with `or`?" — and
`orchid` lexes as `or` followed by `chid`. That word is in
[`clox/test/tokens.lox`](../clox/test/tokens.lox) for exactly that reason, and it
is the first thing [`tool/scandiff.sh`](../tool/scandiff.sh) catches when the
trie is wrong.

## `main.c` stops being a demo

The demo chunk that was `main()` in chapters 14 and 15 is now
[`clox/test/test_chunk.c`](../clox/test/test_chunk.c), a test binary linking the
same objects minus `main.o`, in the same way `test_heap.c` already did. Nothing
about it changed except the name of `interpret`, which is now `interpretChunk`;
`interpret` takes a `const char*`.

Keeping it matters more than it looks. Chapter 16 adds a scanner and removes the
only thing in the program that can execute bytecode, so without the move,
`make test` would stop covering the VM on the day the VM stopped being reachable
from `main`. Hand-written bytecode is the only bytecode there is until chapter 17.

One divergence: `readFile` allocates through `reallocate` rather than `malloc`.
[`clox/memory.h`](../clox/memory.h) states that `reallocate` is the only function
in clox that allocates, and a source buffer is not an exception worth making —
it is what keeps `make test HEAP=own`'s exit-time leak check exact, and it is one
fewer special case for chapter 26's byte counter.

## Challenge 1 — string interpolation

Implemented. `TOKEN_INTERPOLATION` is a string segment that ends at a `${`
rather than at a quote; `TOKEN_STRING` is one that ends at a quote. A consumer
reading a `TOKEN_INTERPOLATION` knows an expression follows, and after that
expression another segment.

The book's example:

```lox
"${drink} will be ready in ${steep + cool} minutes."
```

```
INTERPOLATION  '"${'
IDENTIFIER     'drink'
INTERPOLATION  '} will be ready in ${'
IDENTIFIER     'steep'
PLUS           '+'
IDENTIFIER     'cool'
STRING         '} minutes."'
```

Each segment's lexeme carries both of its own delimiters, so the text is
`start + 1` through `length - 1` for a `STRING` and `length - 2` for an
`INTERPOLATION`. That is uglier than trimming a fixed amount and it is what lets
a consumer tell, from the token alone, whether more of the string is coming.

The nesting question:

```lox
"Nested ${"interpolation?! Are you ${"mad?!"}"}"
```

```
INTERPOLATION  '"Nested ${'
INTERPOLATION  '"interpolation?! Are you ${'
STRING         '"mad?!"'
STRING         '}"'
STRING         '}"'
```

Five tokens, three of them `STRING`, and the last two are the outer strings
resuming and immediately ending. Nothing about this needed a special case: a `"`
inside an interpolated expression starts an ordinary string, which can itself
interpolate.

**What it cost.** Until this challenge the scanner had no memory. Its state was
two pointers and a line number, and
[the list of things it refuses to know](front-end.md#what-the-scanner-refuses-to-know)
was the design: no nesting, no context, no declarations, no types. "No nesting"
is now false. A `}` has to decide whether it closes an interpolation or a block:

```lox
"${ {} }"
```

— so the scanner counts open braces, per open interpolation, on a stack. That is
the first unbounded thing in it, and the reason `MAX_INTERPOLATION` is 8 and a
hard error rather than a growable array: a string eight interpolations deep is
not code anyone meant to write, and saying so is more useful than supporting it.

The stack is also why this is a scanner change rather than a parser one. The
parser could match braces trivially — it does it for blocks already. The scanner
has to, because the text between `}` and the next `${` is a *string*, and only
the scanner can decide whether the characters after a `}` are program or prose.

## Challenge 2 — `>>` and generics

The C++98 problem is maximal munch, the same rule that makes `!=` one token here
instead of two. `vector<vector<string>>` ends in two closing angle brackets that
the scanner has already committed to being a right-shift operator, and there is
no way for the parser to take it back.

Three languages, three different places to fix it:

- **C++11** kept the token and taught the *parser* to reinterpret it. Inside a
  template argument list, a `>>` is treated as two `>`. The token is still
  produced; its meaning is now positional.
- **Java** kept the token and taught the parser to *split* it. `javac`'s scanner
  emits `>>`, and the parser breaks it apart when it is closing type arguments.
  Same idea, done by rewriting the token stream rather than the interpretation.
- **C#** never lexes `>>` at all. The specification defines right-shift as a
  *syntactic* production over two adjacent `>` tokens, so the scanner emits two
  and the parser joins them when it wants a shift. The ambiguity is resolved by
  never creating it.

C# is the interesting one, because it is the only answer that does not require
the parser to know it is being lied to. It also generalises: any operator whose
characters can appear adjacently for unrelated reasons is a candidate for being
assembled in the grammar rather than in the scanner.

Lox has no generics and therefore no version of this problem, but it has the
decision in the same place. Every `match('=')` in `scanToken` is a commitment the
parser cannot undo. That is fine here because no Lox program ever wants `!` and
`=` adjacent and separate — and "no program ever wants that" is the entire
justification, which is worth knowing is the justification.

## Challenge 3 — contextual keywords

Some, with the context that makes them reserved:

| Language | Word | Meaningful where |
|---|---|---|
| C# | `await` | inside an `async` method |
| C# | `value` | inside a property setter |
| C# | `from`, `where`, `select` | inside a LINQ query expression |
| Java | `var` | a local variable declaration's type |
| Java | `yield` | inside a `switch` expression |
| Java | `record`, `sealed`, `permits` | a type declaration |
| Python | `match`, `case` | the head of a match statement |
| JavaScript | `of` | between the binding and the iterable in `for...of` |

The pattern is not a coincidence. Every word in that table was added to a
language that already had users, and every one of them was already a legal
identifier in code that already existed. Contextual keywords exist because
reserving a word retroactively breaks programs, and the cost of that is
measured in other people's source files.

**Pros.** New syntax without breaking old code. That is the whole of it, and it
is enough — Java could not have added `var` any other way.

**Cons.** The scanner stops being able to decide what a token is. Whatever
mechanism resolves it lives further down, so:

- Error messages get worse. `var var = 1;` is legal C#-style and means something
  confusing, and a parser that reaches a contextual keyword in the wrong place
  has to produce a message about a word that is sometimes a name.
- Anything that wants to highlight or fold source without parsing it — an editor,
  a diff viewer, a code search index — can no longer be right. This is why
  `async` renders as a keyword in most editors even when it is a variable.
- The grammar gains cases. C#'s LINQ keywords are contextual in a context that is
  itself introduced by a contextual keyword.

**How to implement one here.** Scan it as `TOKEN_IDENTIFIER`, always, and have
the parser compare the lexeme at the grammar position where the word is
meaningful. In clox that comparison is free — the token already has `start` and
`length`, so it is a `memcmp` at one point in one function. In the Go front end
it is a string comparison against `token.Lexeme`, equally cheap.

The alternative is to feed parser state back into the scanner so it can decide —
the "lexer hack", which is how C compilers resolve typedef names. It works, and
it turns two passes into one mutually recursive one; the diagram at the top of
[the front-end overview](front-end.md) stops being a straight line. That is a
large price for a convenience, and it is why every language in the table above
pays in the parser instead.

## Verification

```sh
make -C clox test                      # three golden diffs + the allocator tests
make -C clox test HEAP=own
make -C clox test MODE=release
make -C clox test MODE=release HEAP=own
make -C clox asan
make -C clox asan HEAP=own
tool/scandiff.sh                       # both scanners, the whole corpus
```

`make test` now pins three things. Two are inherited: `test_chunk`'s stdout and
its execution trace on stderr, unchanged from chapter 15. The third is new and is
the first time the interpreter has been handed a `.lox` file —
[`clox/test/tokens.lox`](../clox/test/tokens.lox) scanned into
[`clox/test/expected-tokens.txt`](../clox/test/expected-tokens.txt). That file is
written to hit every `TokenType` once, plus the inputs most likely to be lexed
wrong: `orchid` and the other keyword prefixes and extensions, every single
letter that starts a keyword on its own, `123.sqrt` and `.5`, a multi-line
string, both `TOKEN_ERROR` cases, and all three interpolation shapes.

`tool/scandiff.sh` is the check the golden file cannot be. It runs all 250
non-benchmark corpus programs through both scanners and requires the same
sequence of token types from each:

```
250 compared, 0 disagreed, 4 skipped
```

The four skips are honest rather than convenient, and they are the limits of the
comparison. Two files scan with an error, where clox emits a `TOKEN_ERROR` and
the Go scanner emits nothing at all — which is the design difference above, and
it is not something a diff can reconcile. Two contain multi-line strings, where
the Go token's `String()` spills across several lines of output and its first
column stops being a token type.

Types only, not lexemes: the Go `-tokens` format is `type lexeme literal`
separated by spaces, and a lexeme can contain spaces, so there is no reliable
column to compare against. That is a weaker check than it could be and it is
still the one that matters — a wrong lexeme boundary almost always changes the
type sequence too. Breaking `checkKeyword`'s length test so that every identifier
beginning with `o` lexes as `or` is caught on the first file.

The Go implementation is untouched and must stay green:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
./tool/booktest.sh
```

## Scope boundary

Challenge 1 is code; 2 and 3 are prose, because neither has any in it.

`test/scanning` is still skipped by [`tool/booktest.py`](../tool/booktest.py),
and chapter 16 does not change that. Those six files expect jlox's chapter-4
output — `// expect: NUMBER 123 123.0` — which clox structurally cannot produce,
because its tokens carry no literal value and never will. Un-skipping them means
reformatting the Go `-tokens` output to the book's, which is a Go-side change
with no C in it.

Two things here have no consumer. `TOKEN_INTERPOLATION` is scanned and nothing
parses it; the earliest it could be compiled is chapter 19, which is where
strings become objects and concatenation exists. And `INTERPRET_COMPILE_ERROR`
is plumbed through `runFile` to exit code 65, which nothing can return yet,
because `compile` reports nothing.

Chapter 17 adds the compiler, and the pressure lands in three places.
`compile`'s signature grows a `Chunk*` and a `bool`, and the token dump in
[`clox/compiler.c`](../clox/compiler.c) goes away — so whatever
`expected-tokens.txt` is protecting has to move behind a flag or into a test
binary, the same problem `main.c`'s demo had this chapter. `TOKEN_ERROR` gets its
first real consumer in `advance`, and the special case in `errorAt` is where the
cost of putting errors in the stream is paid. And `strtod` arrives, which is the
first time a clox token means a value rather than a span — and the first time the
two implementations could be compared on something other than token types.

> It has since landed — see
> [chapter 17](17-compiling-expressions.md#scope-boundary) for the detail. All
> four predictions held. The token dump moved behind a flag rather than into a
> test binary, because [`tool/scandiff.sh`](../tool/scandiff.sh) needs it in the
> shipped binary; the `errorAt` special case is one branch with a comment for a
> body; and `strtod` did make the two implementations comparable, through
> [`tool/rpndiff.sh`](../tool/rpndiff.sh), which checks the emitted bytecode
> against the Go AST printer's reverse-Polish rendering of the same expression.
