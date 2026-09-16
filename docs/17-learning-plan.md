# Chapter 17 learning plan — Compiling expressions

This plan follows
[Crafting Interpreters, Chapter 17](https://craftinginterpreters.com/compiling-expressions.html)
through this repository's C implementation. It is the chapter where the front and
back ends meet: after it, `clox` is a language implementation rather than two
halves of one. It is also the first chapter where the C and Go implementations
can be handed the same program and asked to agree about its *structure*.

## Learning outcomes

By the end, you should be able to:

1. State the entire parser's state, and say what that rules out.
2. Read `parsePrecedence` and say which line is the precedence decision and which
   line is the "is this token even legal here" decision.
3. Explain, without hand-waving, why `binary()` passes `precedence + 1` and
   `unary()` passes `PREC_UNARY`, and predict the value of `1 - 2 - 3` under each
   mistake.
4. Say what `panicMode` does, what it does *not* do, and why chapter 17 has only
   half of an error-recovery mechanism.
5. Name the one branch that chapter 16's "an error is a token" design costs, and
   find it.
6. Explain why the compiler emits constants through `writeConstant` rather than
   `emitBytes(OP_CONSTANT, ...)`, and what a golden window at index 255 proves.
7. Say why bytecode for an expression and a reverse-Polish rendering of its AST
   are the same sentence, and what that makes testable.
8. Explain what a mixfix operator needs from a Pratt parser (the answer is
   "nothing").

The C implementation needs no toolchain setup — the Makefile uses `cc` and the
book's own flags:

```sh
make -C clox test
```

For the Go side, use this command prefix in the current environment:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache
```

## Session 1 — The loop closes

Read §17.1, then use it before reading any of it:

```sh
make -C clox
./clox/build/debug-libc/clox
> 1 + 2 * 3
> (1 + 2) * 3
> -1 + 2
```

```sh
./clox/build/debug-libc/clox test/expressions/evaluate.lox
```

That last file has been in the corpus, unused, since it was vendored. Open it.
It holds one expression and a `// expect:` comment, and it is the first file in
the book's own test suite that clox can run.

Now look at the seam:

```sh
./clox/build/debug-libc/clox -dump  test/expressions/evaluate.lox
./clox/build/debug-libc/clox -trace test/expressions/evaluate.lox
```

- `-dump` writes to stdout; `-trace` writes to stderr. Read
  [`clox/debug.h`](../clox/debug.h) and say why the same function serves both and
  why the stream is a parameter.
- Find the three lines in [`clox/vm.c`](../clox/vm.c) that are the whole of
  `interpret` now. What happens to the `Chunk` if `compile` returns `false`?

Checkpoint: chapter 16's `runFile` had a path to exit code 65 that nothing could
reach. Reach it:

```sh
./clox/build/debug-libc/clox clox/test/bad.lox; echo $?
```

Then say which function sets the flag that produced it, and how many callers read
that flag. (One.)

## Session 2 — Two tokens, and what that costs

Read §17.2 and the `Parser` struct.

Write down the complete state of the parser. It is four fields. Then open
[`parser/parser.go`](../parser/parser.go) and write down the complete state of
the other one — it is a slice and an index.

Answer from the code:

- `parser/parser.go:481-491` parses an assignment target by parsing an ordinary
  expression first and then deciding, after the fact, whether what it built was
  assignable. Explain why `clox` cannot do that, in one sentence about `previous`.
- Chapter 21 solves it with a `bool canAssign` parameter threaded through every
  parse function. Say which direction information flows in each design.

Now count frames. For the source `1`, list every function the Go recursive-descent
parser enters, in order. Then list every function `clox` enters. (Eleven against
three.)

Checkpoint: the chapter's algorithm is a table plus a loop, and its constraint is
one token of lookahead. Decide which of those two is the cause and which is the
effect, then read
[the chapter notes](17-compiling-expressions.md#two-tokens-is-the-whole-parser-state).

## Session 3 — Precedence is a number

Read §17.6, `parsePrecedence`, and the `rules` table.

Predict the bytecode for each of these before running anything:

```
1 + 2 * 3
(1 + 2) * 3
-1 + 2
-(1 + 2)
```

```sh
for e in '1 + 2 * 3' '(1 + 2) * 3' '-1 + 2' '-(1 + 2)'; do
  printf '%s' "$e" > /tmp/e.lox
  echo "== $e"; ./clox/build/debug-libc/clox -dump /tmp/e.lox | tail -n +2
done
```

Then answer, pointing at lines:

- Which single line in `parsePrecedence` compares precedence? Which line decides
  that a token cannot start an expression at all?
- `grouping()` emits no bytecode. Where did the parentheses go?
- `unary()` emits `OP_NEGATE` *after* compiling its operand. Say why that order
  is forced rather than chosen.

**Break it.** In [`clox/compiler.c`](../clox/compiler.c), make `unary` parse its
operand as a full expression:

```c
-  parsePrecedence(PREC_UNARY);
+  expression();
```

```sh
make -C clox test
```

```
 == -1 + 2 ==
 0000    1 OP_CONSTANT         0 '1'
-0002    | OP_NEGATE
-0003    | OP_CONSTANT         1 '2'
-0005    | OP_ADD
+0002    | OP_CONSTANT         1 '2'
+0004    | OP_ADD
+0005    | OP_NEGATE
 0006    | OP_RETURN
-1
+-3
```

`-1 + 2` is now `-(1 + 2)`. Put it back.

Checkpoint: say what `PREC_UNARY` means as an English sentence about `-a.b + c`,
and check it against the comment above that line.

## Session 4 — `+ 1` is associativity

Read the `binary()` function, specifically this:

```c
ParseRule* rule = getRule(operatorType);
parsePrecedence((Precedence)(rule->precedence + 1));
```

Predict the value of `1 - 2 - 3` if the `+ 1` were not there. Then predict
`12 / 3 / 2`, `8 / 4 * 2` and `1 + 2 - 3 + 4`.

**Break it:**

```c
-  parsePrecedence((Precedence)(rule->precedence + 1));
+  parsePrecedence(rule->precedence);
```

```sh
make -C clox test
```

```
 == 1 - 2 - 3 ==
 0000    1 OP_CONSTANT         0 '1'
 0002    | OP_CONSTANT         1 '2'
-0004    | OP_SUBTRACT
-0005    | OP_CONSTANT         2 '3'
+0004    | OP_CONSTANT         2 '3'
+0006    | OP_SUBTRACT
 0007    | OP_SUBTRACT
 0008    | OP_RETURN
--4
+2
```

Then run the differential, which has never seen the golden file and reaches the
same verdict from the other direction:

```sh
tool/rpndiff.sh
```

```
FAIL 1 - 2 - 3
  go   1 2 - 3 -
  clox 1 2 3 - -
...
30 compared, 10 disagreed
```

Ten of thirty. Put it back and confirm `30 compared, 0 disagreed`.

Now do the same question in the other language. Open `term` in
[`parser/parser.go`](../parser/parser.go) (`:573`) and change its `for` to an
`if` with a recursive call to `p.term()`, so it recurses right instead of looping
left:

```sh
env GOTOOLCHAIN=go1.26.6 go test ./parser/...
```

```
--- FAIL: TestParse (0.00s)
    parser_test.go:146: "1 - 2 - 3":
          got  (- 1 (- 2 3))
          want (- (- 1 2) 3)
--- FAIL: TestAlgorithmsAgree (1.06s)
--- FAIL: TestParseClassesPropertiesAndThis (0.00s)
```

Same expression, same wrong tree, reached by removing a `for` instead of a `+ 1`.
`TestAlgorithmsAgree` is the one worth noting: the LL(k) parser was not touched,
so the two Go front ends now disagree with each other — which is the same kind of
check `rpndiff.sh` is, one language earlier. Put it back.

Checkpoint: two implementations, two spellings of one idea — `+ 1` and `for`.
Write the third spelling, the one
[`docs/llk-parser.md`](llk-parser.md) describes, where associativity is decided
by *where the semantic action sits inside the production*. Three encodings, one
language.

## Session 5 — Errors, and the half of recovery this chapter has

Read §17.2's error section: `errorAt`, `error`, `errorAtCurrent`, and `advance`.

Answer from the code:

- `advance` contains a `for (;;)` loop. What is it looping over, and why can the
  rest of the parser then assume `TOKEN_ERROR` does not exist?
- `errorAt` has a branch for `TOKEN_ERROR` whose body is a comment. Read
  [`clox/scanner.h`](../clox/scanner.h) on what a `TOKEN_ERROR`'s `start` points
  at, and say what would print without that branch.
- `error` reports at `previous`, `errorAtCurrent` at `current`. Find a call site
  of each and say why it chose the one it did.

**Break it.** Delete the two lines that implement panic mode:

```c
-  if (parser.panicMode) return;
-  parser.panicMode = true;
```

```sh
make -C clox test
```

```
 -- 1 + + 2
 [line 1] Error at '+': Expect expression.
+[line 1] Error at '2': Expect end of expression.
 -- @
 [line 1] Error: Unexpected character.
+[line 1] Error at end: Expect expression.
...
 -- 1 ? 2
 [line 1] Error at end: Expect ':' after then branch of conditional.
+[line 1] Error at end: Expect expression.
+[line 1] Error at '?': The '?:' operator parses but cannot be compiled yet.
```

Note what did *not* change: every one of those still compiles to the same
nothing, and `hadError` was already true. Put it back.

Checkpoint: `panicMode` suppresses messages and nothing else — the parser keeps
parsing either way. The other half of recovery is `synchronize`, which
[`parser/parser.go:449-462`](../parser/parser.go) already has and this chapter
does not. Read it, then say why chapter 17 *cannot* have it. (What would it
synchronise to?)

## Session 6 — The chunk, and where the line numbers come from

Read §17.3 and `emitByte`, `emitConstant` and `endCompiler`.

`emitConstant` calls `writeConstant`, which is chapter 14's challenge 2, rather
than the book's `emitBytes(OP_CONSTANT, makeConstant(value))`. Find the window in
[`clox/test/expected-compile.txt`](../clox/test/expected-compile.txt) where the
operand widens:

```
0764    | OP_CONSTANT       255 '255'
0766    | OP_ADD
0767    | OP_CONSTANT_LONG  256 '256'
-- 988 code bytes, 300 constants
44850
```

- Why does that test print the *sum* as well as the instructions? What would a
  disassembly alone fail to catch?
- `writeConstant` returns `bool` now, where chapter 14 called `exit(1)`. Find the
  comment in [`clox/chunk.c`](../clox/chunk.c) that predicted this chapter, and
  the one that explains why the bound is checked before the value is interned.
- The book's `emitBytes` is not in this file. Work out why, from the fact that it
  had exactly one caller.

**Break it.** Blame each byte on the wrong token:

```c
-  writeChunk(currentChunk(), byte, parser.previous.line);
+  writeChunk(currentChunk(), byte, parser.current.line);
```

```sh
make -C clox test
```

```
 == 1 + 2 <newline> - 3 ==
 0000    1 OP_CONSTANT         0 '1'
 0002    | OP_CONSTANT         1 '2'
-0004    | OP_ADD
-0005    2 OP_CONSTANT         2 '3'
+0004    2 OP_ADD
+0005    | OP_CONSTANT         2 '3'
 0007    | OP_SUBTRACT
 0008    | OP_RETURN
```

This one is worth dwelling on, because the obvious test does not catch it. A
source of `1\n+ 2` passes under both versions — work out why before reading the
comment above that case in
[`clox/test/test_compile.c`](../clox/test/test_compile.c). The answer is about
when `OP_ADD` is emitted relative to the tokens either side of it.

Checkpoint: nothing reads these line numbers yet. Say which chapter makes them
user-visible, and what `getLine`'s linear walk will then be on the path of.

## Session 7 — Two parsers, one expression

Read [`tool/rpndiff.sh`](../tool/rpndiff.sh), then run it:

```sh
tool/rpndiff.sh -v
```

The claim it rests on is one sentence: bytecode for an expression *is* its
postfix traversal. Convince yourself with one example —

```sh
printf '(-1 + 2) * 3 - -4'  > /tmp/c.lox
printf '(-1 + 2) * 3 - -4;' > /tmp/g.lox
./clox/build/debug-libc/clox -dump /tmp/c.lox
env GOTOOLCHAIN=go1.26.6 go run . -print=rpn /tmp/g.lox
```

— and then answer:

- Where did the parentheses go, on each side? Name the function in each
  implementation that drops them.
- Why does the Go printer spell unary minus `negate` instead of `-`? Read
  `VisitUnaryExpr` in [`ast/rpn.go`](../ast/rpn.go). What would break otherwise,
  and does the same problem exist in bytecode? (`OP_NEGATE` and `OP_SUBTRACT` are
  different bytes.)
- The script compares only arithmetic on small number literals. List the two
  reasons, and say which one goes away in chapter 18 and which one gets worse.

Checkpoint: `scandiff.sh` compares token types across 250 files; `rpndiff.sh`
compares operand order across 30 expressions. Say what each one would catch that
the other cannot, and then name the thing *neither* can check — the one that
needs both implementations to agree on a printed value rather than a structure.

## Session 8 — Challenge 1, and the parser explaining itself

Read challenge 1 and write the trace by hand, on paper, for
`(-1 + 2) * 3 - -4`. Which functions, in what order, called by whom, with what
arguments.

Then check it:

```sh
make -C clox parsetrace
```

Four things to look for in the output, all of which the hand-written version
usually gets at least one of wrong:

1. The argument to `parsePrecedence` is never the calling operator's own
   precedence.
2. `binary(PLUS)` and `binary(STAR)` are at different depths, and the reason is
   `grouping`.
3. `unary(MINUS)` and `binary(MINUS)` appear three lines apart on the same token
   type.
4. The last `binary(MINUS)` is looking at a `-` that will be a *negation*.

Read [the chapter notes](17-compiling-expressions.md#challenge-1--the-parser-traced)
for what each of those four is showing.

Checkpoint: trace the ternary, predicting the shape first — how deep does the
middle operand go, and at what precedence does the else branch start?

```sh
make -C clox parsetrace PARSE_TRACE_EXPR='1 ? 2 * 3 : 4'
```

```
  ternary() previous='?' current='2'
    parsePrecedence(ASSIGNMENT) previous='?' current='2'
      ...
    parsePrecedence returns
    parsePrecedence(CONDITIONAL) previous=':' current='4'
```

`ASSIGNMENT` for the middle operand and `CONDITIONAL` — not one tighter — for the
else branch. Say what each of those two choices buys, and check your answer
against [challenge 3](17-compiling-expressions.md#challenge-3--a-mixfix-operator).

## Challenges

Two are code, one is research.

1. **Implemented** — trace the parse of `(-1 + 2) * 3 - -4`. See
   [the chapter notes](17-compiling-expressions.md#challenge-1--the-parser-traced)
   for the trace the parser produces about itself, and `make -C clox parsetrace`
   to produce it for any expression.

   The extension: the trace is entry and exit only. Add the bytes as they are
   emitted, interleaved at the right depth, and you have a rendering of *why*
   each instruction is where it is. That is roughly what a real compiler's
   `-fdump-tree-*` does, and it is about fifteen lines here.

2. **Implemented, as prose** — which tokens are both prefix and infix. See
   [the chapter notes](17-compiling-expressions.md#challenge-2--prefix-and-infix-in-one-row).
   In full Lox the answer is exactly two, `-` and `(`, and the second one is the
   interesting one because the Go parser cannot state it in one place at all.

   The exercise worth doing is C's `(`: write down the two parses of `(x)*y` and
   say what the parser needs to know to choose, then find out what that
   requirement is called and why it makes C impossible to parse without a symbol
   table.

3. **Implemented** — the ternary `?:`. See
   [the chapter notes](17-compiling-expressions.md#challenge-3--a-mixfix-operator).
   It parses, at the right precedences and with the right associativity, and
   emits nothing, because choosing between two operands needs a jump.

   The extension is to finish it in chapter 23. Come back here after
   `OP_JUMP_IF_FALSE` exists, and note that the *parser* part of this challenge
   will not need to change at all — which is the claim the rules table has been
   making since it was written.
