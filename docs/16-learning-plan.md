# Chapter 16 learning plan — Scanning on demand

This plan follows
[Crafting Interpreters, Chapter 16](https://craftinginterpreters.com/scanning-on-demand.html)
through this repository's C implementation. It is the first chapter where clox
reads a `.lox` file — and the first where the two implementations in this
repository do the same job and can be checked against each other.

## Learning outcomes

By the end, you should be able to:

1. Say what "on demand" buys, and give the reason that is *not* memory.
2. Explain why a `Token` holding a pointer into the source constrains when the
   source may be freed, and name the function that respects it.
3. Say why `123` is still text after scanning, and which implementation can
   report a number-literal overflow because of that.
4. Explain why keywords are matched *after* a maximal run is scanned, and name
   the word that proves it.
5. Say why a trie beats a hash map here, in terms of bytes touched.
6. Explain what a `TOKEN_ERROR` is for, and what the alternative design can
   forget to do.
7. Say what string interpolation costs the scanner that nothing before it did.
8. Name what `tool/scandiff.sh` cannot compare, and why.

The C implementation needs no toolchain setup — the Makefile uses `cc` and the
book's own flags:

```sh
make -C clox test
```

For the Go side, use this command prefix in the current environment:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache
```

## Session 1 — A program instead of a demo

Read §16.1, then build and use it:

```sh
make -C clox
./clox/build/debug-libc/clox
> print 1 + 2;
```

```sh
./clox/build/debug-libc/clox test/precedence.lox | head
```

Then read [`clox/main.c`](../clox/main.c) and answer:

- `runFile` frees the source *after* `interpret` returns. Find the comment in
  [`clox/scanner.h`](../clox/scanner.h) that says why that ordering is not a
  style choice.
- The REPL's buffer is 1024 bytes. What happens to a longer line? Try it:
  `python3 -c 'print("var x = 1; " * 200)' | ./clox/build/debug-libc/clox`
- `readFile` calls `reallocate`, not `malloc`, which is a divergence from the
  book. Read [`clox/memory.h`](../clox/memory.h) and say what the divergence
  protects.

Now find the demo. Chapters 14 and 15 had four hand-built chunks in `main()`,
and `main()` is a REPL now. Where did they go, and what would `make -C clox test`
have stopped covering if they had simply been deleted?

Checkpoint: `runFile` exits 65 on `INTERPRET_COMPILE_ERROR`. Nothing in this
chapter can return that. Write down what has to exist before it can, and then
check [`tool/booktest.py`](../tool/booktest.py) for where the number 65 already
appears.

## Session 2 — One token, when asked

Read §16.2 and `scanToken` in [`clox/scanner.c`](../clox/scanner.c), then the
loop in [`clox/compiler.c`](../clox/compiler.c).

Predict the exact dump before running it:

```lox
var x = 1.5;
// comment
print x != 2;
```

Check with:

```sh
printf 'var x = 1.5;\n// comment\nprint x != 2;\n' > /tmp/t.lox
./clox/build/debug-libc/clox /tmp/t.lox
```

Three things to get right, and each is a different mechanism: the comment
produces no token at all, `!=` produces one token and not two, and `1.5` produces
one token whose text is `1.5` and whose *value* is nowhere.

Now compare against the other scanner:

```sh
env GOTOOLCHAIN=go1.26.6 go run . -tokens /tmp/t.lox
```

Same tokens, different columns. Say which column the Go scanner has that clox
cannot have, and read `number` in [`clox/scanner.c`](../clox/scanner.c) and
`number` in [`lexer/scanner/scanner.go`](../lexer/scanner/scanner.go) to find the
one line that is the difference.

Checkpoint: the chapter's claim is that scanning on demand saves memory. Work out
how much, exactly, for the largest file in the corpus — `sizeof(Token)` is 24 in
C and 56 in Go, and `./clox/build/debug-libc/clox test/limit/too_many_constants.lox | wc -l`
is the token count. Then decide whether that is the real reason, and read
[the chapter notes](16-scanning-on-demand.md#on-demand-is-about-memory-and-it-is-a-smaller-number-than-it-sounds)
for three that are better.

## Session 3 — Maximal munch and the trie

Read §16.4, `identifierType` and `checkKeyword`.

Predict, then check with `./clox/build/debug-libc/clox clox/test/tokens.lox`:

- `orchid` — one token or two?
- `andy`, `classy`, `printer`, `format` — which of these is a keyword?
- `f`, `fo`, `fu`, `fa` — what stops `checkKeyword` from reading past the end of
  a one-character identifier?

Now break the trie, in the way that is easiest to do by accident — drop the
length test for a single-keyword branch:

```c
    case 'o': return TOKEN_OR;
```

`make -C clox test` fails on the golden file, which tells you *that* something
changed. Run the other check to find out how much:

```sh
tool/scandiff.sh
```

```
      go   IDENTIFIER
      clox OR
```

That is every identifier in the corpus beginning with `o` — `other`, `object`,
`outer` — now lexing as the keyword `or`. The golden file caught it in one word;
the differential caught it in a hundred programs. Put it back.

Checkpoint: the trie and the Go scanner's `map[string]TokenType` agree on every
input. Say what the trie is actually faster at, in bytes touched, and then say
why that answer would change if Lox had two hundred keywords instead of sixteen.

## Session 4 — Whitespace is where the line numbers live

Read `skipWhitespace`. Note that a comment is handled there rather than being
scanned into a token that is thrown away, and say why the `/` case needs two
characters of lookahead when no other case needs any.

Then break the line counter:

```c
      case '\n':
        advance();
        break;
```

```sh
make -C clox test
```

```
-   4 LEFT_PAREN     '('
+   1 LEFT_PAREN     '('
...
-   5 BANG           '!'
+   | BANG           '!'
```

Every token in the file now claims to be on line 1, and the dump's bar-instead-of-
a-number trick turns the whole program into one block. The tokens themselves are
all still correct — this is purely a bug in what an error message would say.

Put it back, then find the *other* place a line number is maintained by hand, in
`string`, and say what breaks if it is missing there instead. Check your answer
against `"line one\nline two"` in [`clox/test/tokens.lox`](../clox/test/tokens.lox)
— and notice which of the two lines the golden file reports for that token.

Checkpoint: `getLine` in chapter 14 stored lines as runs because bytecode is
generated in source order and a run of bytes shares a line. The token dump uses
the same trick for display. Say whether a token stream has the same property
bytecode does, and whether a *token* could have been stored that way.

## Session 5 — An error is a token

Read `errorToken`, then read
[the "scanner never fails" section](front-end.md#the-scanner-never-fails) of the
front-end overview, which describes the other implementation's answer to the same
problem.

Answer from the code, not the prose:

- What does `token.start` point at for a `TOKEN_ERROR`? What breaks if a consumer
  treats it like any other token's `start`?
- The Go scanner returns a slice and sets a global. Name the way a caller can get
  that wrong, and then find the commit that got it wrong (`git log --oneline
  --grep 'scanner error'`).

Now break the consumer rather than the scanner. In
[`clox/compiler.c`](../clox/compiler.c), stop at the first error:

```c
    if (token.type == TOKEN_EOF || token.type == TOKEN_ERROR) break;
```

```sh
make -C clox test
```

```
   38 ERROR          'Unexpected character.'
-   | ERROR          'Unexpected character.'
-   | IDENTIFIER     'keepgoing'
-  42 ERROR          'Unterminated string.'
-   | EOF            ''
```

One bad character cost the rest of the file. That is the behaviour the Go
implementation had, on purpose, until commit `0bb2ad8` — and
[`test/unexpected_character.lox`](../test/unexpected_character.lox) is the test
that exists because of it. Read it. Put the loop back.

Checkpoint: clox's design makes it impossible to forget the error and awkward to
report it — chapter 17's `errorAt` needs a special case for a token whose `start`
is not in the source. The Go design is the reverse. Decide which you would pick
for a compiler with twenty error sites, and say what changes your answer.

## Session 6 — What interpolation costs

Read challenge 1: the `string` function, the `'{'` and `'}'` cases in
`scanToken`, and the `braces` array on `Scanner`.

Predict the token sequence for each, then check them in the golden file:

```lox
"${drink} will be ready in ${steep + cool} minutes."
"Nested ${"interpolation?! Are you ${"mad?!"}"}"
"${ {} }"
```

The second one is the challenge's own question and has a surprising answer: three
of its five tokens are `STRING`.

Now break the brace counter. Make every `}` end an interpolation:

```c
    case '}':
      if (scanner.interpolation > 0) {
        scanner.interpolation--;
        return string();
      }
      return makeToken(TOKEN_RIGHT_BRACE);
```

```sh
make -C clox test
```

```
   34 INTERPOLATION  '"${'
    | LEFT_BRACE     '{'
-   | RIGHT_BRACE    '}'
-   | STRING         '}"'
+   | STRING         '} }"'
```

The block's closing brace was eaten as the end of the interpolation, and
everything after it — including the real closing brace — became string text. Put
it back.

Checkpoint: before this challenge the scanner's entire state was two pointers and
a line number. Re-read
[what the scanner refuses to know](front-end.md#what-the-scanner-refuses-to-know)
and say which item on that list is now untrue. Then answer the harder question:
the parser already matches braces for blocks, so why can it not match these?

## Session 7 — Two scanners, one corpus

Run the whole thing:

```sh
make -C clox test
make -C clox test HEAP=own
make -C clox test MODE=release
make -C clox test MODE=release HEAP=own
make -C clox asan
make -C clox asan HEAP=own
tool/scandiff.sh -v
```

```
250 compared, 0 disagreed, 4 skipped
```

Read [`tool/scandiff.sh`](../tool/scandiff.sh) and answer:

- Why does it compare token *types* and not lexemes? The answer is in the Go
  `Token.String()` format — find it in
  [`lexer/token/token.go`](../lexer/token/token.go).
- Two of the four skipped files scan with an error. Why is that not something a
  diff can reconcile, rather than something the script is being lazy about?
- Two more contain a multi-line string. What goes wrong in the Go output, and
  what does [`clox/compiler.c`](../clox/compiler.c) do to avoid the same problem?

Then confirm the Go side is untouched:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
./tool/booktest.sh
```

Now the gap. [`tool/booktest.py`](../tool/booktest.py) skips `test/scanning` with
the comment "No interpreter yet in those chapters." There is one now. Read
[`test/scanning/keywords.lox`](../test/scanning/keywords.lox) and say why clox
still cannot pass it, and what would have to change — and in which
implementation.

Final checkpoint: chapter 14's notes said the shared test corpus "returns in
chapter 16". It has, partly: 250 programs now go through both implementations,
and what is compared is the weakest thing both can say. Write down the next thing
that becomes comparable, and which chapter adds it.

## Challenges

One is code, two are research.

1. **Implemented** — string interpolation. See
   [the chapter notes](16-scanning-on-demand.md#challenge-1--string-interpolation)
   for the token types, both requested sequences, and what the brace stack cost.

   The extension it is short of is the one that matters for chapter 19: nothing
   *consumes* these tokens. Work out what the compiler would emit for
   `"${a} and ${b}"` once string concatenation exists, and count the
   `OP_ADD`s. Then decide whether a dedicated `OP_CONCAT` taking a count would be
   worth an opcode — the answer connects directly to
   [chapter 15's challenge 2](15-a-virtual-machine.md#challenge-2--a-minimal-instruction-set).

2. **Implemented, as prose** — `>>` and generics. See
   [the chapter notes](16-scanning-on-demand.md#challenge-2---and-generics).
   Three languages put the fix in three different places, and C#'s is the only
   one that does not require the parser to know it is being lied to.

3. **Implemented, as prose** — contextual keywords. See
   [the chapter notes](16-scanning-on-demand.md#challenge-3--contextual-keywords).
   The exercise worth doing is to add one: make `await` a contextual keyword in
   the Go front end, which has a parser to put it in, and see how far the change
   spreads. It should be one comparison in one function. Find out whether it is.
