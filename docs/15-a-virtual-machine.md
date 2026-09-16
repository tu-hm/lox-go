# Chapter 15 — A virtual machine

Implementation of
[A Virtual Machine](https://craftinginterpreters.com/a-virtual-machine.html).
Chapter 14 built a data structure and a disassembler and ran nothing. This adds
the thing that runs it.

> Status: implemented, including all four challenges. The VM holds an
> instruction pointer, a value stack that grows rather than overflowing, and a
> dispatch loop over eight opcodes. Execution can be traced instruction by
> instruction. Bytecode is still written by hand — chapter 17 is where a
> compiler produces it.

For a guided walkthrough, follow the [Chapter 15 learning plan](15-learning-plan.md).

## What the chapter adds

| Type | Shape | What it is |
|---|---|---|
| `VM` | `chunk`, `ip`, `stack`, `stackTop`, `stackCapacity`, `trace` | the whole machine |
| `InterpretResult` | `OK`, `COMPILE_ERROR`, `RUNTIME_ERROR` | what `interpret` reports |

| Opcode | Operand | Stack effect |
|---|---|---|
| `OP_ADD` | none | pop 2, push 1 |
| `OP_SUBTRACT` | none | pop 2, push 1 |
| `OP_MULTIPLY` | none | pop 2, push 1 |
| `OP_DIVIDE` | none | pop 2, push 1 |
| `OP_NEGATE` | none | replace the top |
| `OP_RETURN` | none | pop 1 and print it |

`OP_RETURN` printing its operand is a placeholder and the book says so. It is
the only way to observe anything until chapter 21 adds `print`.

## The loop is the program

[`run`](../clox/vm.c) is a `for (;;)` around a `switch` on one byte. That is the
entire interpreter, and everything the rest of part III does is either add cases
to that switch or change what the cases are allowed to assume.

Three things about it are worth saying out loud, because they are easy to read
past:

**There is no program counter in the `switch`.** `READ_BYTE()` is
`(*vm.ip++)` — the advance is a side effect of the fetch. An instruction that
wants an operand fetches it the same way, so the operand is consumed by whoever
knows it is there. Nothing else in the loop knows how wide anything is.

**Dispatch is the cost, not the work.** `OP_ADD` is one floating-point
instruction wrapped in a fetch, a bounds-free array index, a jump through a
table, and two stack writes. That ratio is why part III's later chapters spend
so much effort on removing *instructions* rather than on making them faster, and
why the book's benchmarks live where they do.

**The loop cannot fail.** There is no error path in chapter 15 because there is
nothing yet that can go wrong: every value is a `double`, so every operand is
valid for every operator. `1 / 0` is `inf`, not an error. Chapter 18 adds types
and with them the first runtime error, and that is when `getLine` finally gets a
caller that is not the disassembler.

## `ip` is a pointer, and that is a decision

The book makes `vm.ip` a `uint8_t*` into the middle of `chunk->code` rather than
an integer offset, and the reason given is dereference speed. That is true and
it is the smaller half of the reason.

The larger half is that an index would need `vm.chunk` on every single fetch —
`vm.chunk->code[vm.ip++]` is two loads before the byte, one of them a pointer
chase through the VM struct. The pointer form is one load. On a loop that runs
once per instruction, that is the difference the chapter is buying.

What it costs is that `ip` is only meaningful while `chunk->code` stays where it
is. Nothing grows a chunk while it is running today. Chapter 24 is where a chunk
belongs to a function and the VM switches between them, and that is when the
invalidation rule has to become explicit rather than incidental.

The same hazard, in a form that does bite immediately, is `stackTop` — see
challenge 3.

## The stack top points at nothing

`stackTop` points at the next *free* slot, not at the top value. Empty is
`stackTop == stack`, which needs no special case, and `push` never has to ask
whether the stack is empty before writing.

The cost is that reading the top value is `stackTop[-1]`, which looks like an
out-of-bounds access and is not. That spelling shows up once here, in challenge
4's in-place negate, and constantly from chapter 21 onwards.

## Two decoders for one operand

The book's `READ_CONSTANT()` reads one byte. This repository's chunks can also
carry `OP_CONSTANT_LONG`, chapter 14's challenge 2, whose operand is three bytes
little-endian — so the dispatch loop has to decode it, and it is now the second
decoder for that encoding. The first is `constantLongInstruction` in
[`clox/debug.c`](../clox/debug.c).

Two decoders for one format is a bug waiting to happen, and the golden test is
what makes it not one: the disassembly and the executed result are printed from
the same chunk, so a disagreement shows up as a number that does not match its
own listing.

The decode itself is three statements, deliberately:

```c
int constant = READ_BYTE();
constant |= READ_BYTE() << 8;
constant |= READ_BYTE() << 16;
```

Written as one expression — `READ_BYTE() | (READ_BYTE() << 8) | ...` — it
compiles, runs, and is wrong. C does not specify the order in which the operands
of `|` are evaluated, and `READ_BYTE()` has a side effect, so the three bytes may
be read in any order. Whether they are is a property of the optimiser, which
means it can be correct in a debug build and permuted at `-O3`.

## The trace is not free, so it is off by default

`DEBUG_TRACE_EXECUTION` disassembles every instruction as it runs and prints the
stack beside it. The book defines it unconditionally in `common.h` and tells you
to comment it out. Here it is two switches rather than one:

- **Compile time.** `common.h` defines it under `#ifdef DEBUG`, so a release
  build does not contain the branch at all.
- **Run time.** `vmSetTrace(FILE*)` decides where it goes, and `NULL` turns it
  off.

The run-time half is not ceremony. The trace prints the *whole stack* before
every instruction, so tracing a chunk that pushes *n* values costs O(n²)
characters. The demo's last chunk pushes 300, and tracing it would produce
roughly 45,000 stack slots of output for 988 bytes of code. A debugging aid that
cannot be aimed is one you stop using.

And the trace goes to **stderr**, not stdout. That is a divergence from the book,
and it is the same trick [`clox/main.c`](../clox/main.c) already used for the
heap leak check: stdout stays byte for byte identical across every build
configuration, which is exactly the claim `make test HEAP=own` and
`make test MODE=release` exist to check. A trace on stdout would have made the
release build's golden output differ from the debug build's for reasons that have
nothing to do with the program.

## Challenge 1 — what the compiler will emit

The bytecode for the four expressions, which is worth doing on paper because
chapter 17 has to generate exactly this and there is no other way to know whether
it did:

```
1 * 2 + 3            1 + 2 * 3            3 - 2 - 1
  OP_CONSTANT 1        OP_CONSTANT 1        OP_CONSTANT 3
  OP_CONSTANT 2        OP_CONSTANT 2        OP_CONSTANT 2
  OP_MULTIPLY          OP_CONSTANT 3        OP_SUBTRACT
  OP_CONSTANT 3        OP_MULTIPLY          OP_CONSTANT 1
  OP_ADD               OP_ADD               OP_SUBTRACT
```

```
1 + 2 * 3 - 4 / -5
  OP_CONSTANT 1   OP_CONSTANT 2   OP_CONSTANT 3   OP_MULTIPLY   OP_ADD
  OP_CONSTANT 4   OP_CONSTANT 5   OP_NEGATE       OP_DIVIDE     OP_SUBTRACT
```

The thing to notice is that the opcodes come out in postfix order and the
operands in source order. That is not a coincidence and it is not a choice: a
stack machine's instruction sequence *is* the postorder traversal of the syntax
tree.

[`ast/rpn.go`](../ast/rpn.go) has been printing that traversal since chapter 5,
when it was a challenge about the visitor pattern:

```
$ go run . -print=rpn
> print 1 + 2 * 3 - 4 / -5;
(print 1 2 3 * + 4 5 negate / -)
```

Line that up against the bytecode above. It is the same sequence, with the
opcodes spelled as symbols. Chapter 5's challenge turns out to have been a
bytecode compiler for a machine that did not exist yet.

Note also that the left operand is always pushed first and therefore ends up
*deeper*. That is why `BINARY_OP` pops `b` before `a`. Pop them the other way
round and `3 - 2 - 1` evaluates to `2`, and `1 + 2 * 3` still evaluates to `7` —
which is the trap. The bug is invisible for `+` and `*`, so a test suite that
leans on addition will report a working interpreter.

## Challenge 2 — a minimal instruction set

`4 - 3 * -2` three ways, with byte counts:

| | instructions | bytes |
|---|---|---|
| both opcodes | `CONST 4`, `CONST 3`, `CONST 2`, `NEGATE`, `MULTIPLY`, `SUBTRACT` | 9 |
| no `OP_NEGATE` | `CONST 4`, `CONST 3`, `CONST 0`, `CONST 2`, `SUBTRACT`, `MULTIPLY`, `SUBTRACT` | 11 |
| no `OP_SUBTRACT` | `CONST 4`, `CONST 3`, `CONST 2`, `NEGATE`, `MULTIPLY`, `NEGATE`, `ADD` | 10 |

All three produce `10`.

Dropping `OP_NEGATE` is the worse of the two. Negation becomes `0 - x`, which
costs a two-byte `OP_CONSTANT` and a pool entry for zero — and it costs them at
*every* negation, in every chunk, forever. Dropping `OP_SUBTRACT` costs a
one-byte `OP_NEGATE` per subtraction and no pool entry.

So the answer is yes, keep both, and the reason is not opcode space. The enum
has eight entries in a byte with 248 free; nothing here is short of encodings.
The reason is that both instructions are *common*, and every instruction removed
from the set is an instruction added to the stream. A VM's cost is dominated by
dispatch count, so a smaller instruction set is a slower interpreter, not a
smaller one.

Which is also the answer to "any other redundant instructions you would
consider". This repository already has one: `OP_CONSTANT_LONG` is pure
redundancy — `OP_CONSTANT` could have taken three bytes always and the long form
would be unnecessary. It exists because the common case is worth two bytes
instead of four. Every specialised instruction in a real VM is that same trade,
and the book makes it again in chapter 18 with `OP_TRUE`, `OP_FALSE` and
`OP_NIL`, each of which is an `OP_CONSTANT` that costs one byte and no pool
entry.

## Challenge 3 — a stack that grows

The book's stack is `Value stack[STACK_MAX]` with no overflow check, which makes
a wrong chunk undefined behaviour with no diagnostic. Here it is a pointer, a
capacity, and `GROW_ARRAY` — so the stack goes through `reallocate` like every
other array in the program, and joins the single allocation choke point that
chapter 26 will read.

The whole exercise is three lines, and they are the three lines it is about:

```c
int height = vm.stack == NULL ? 0 : (int)(vm.stackTop - vm.stack);
/* ... grow ... */
vm.stackTop = vm.stack + height;
```

`stackTop` is a pointer *into* the buffer being reallocated. A realloc that
moves it leaves `stackTop` dangling — and dangling quietly, because the old
address stays mapped and readable for a while, so the failure is a wrong value
rather than a crash. This is the same class of bug as `ip` in the section above,
except that this one is reachable today.

**Costs and benefits.** The benefit is that no program can corrupt the
interpreter by being too deep. The costs are three, in increasing order of
importance:

1. A branch in `push`. Predictable to the point of free, and gone in the common
   case where the stack never grows — the initial capacity is the book's 256, so
   anything the book's VM could run never reallocates at all.
2. An indirection. `vm.stack` is a pointer load before the write, where the
   book's array is a fixed offset from `vm`. This is the real cost.
3. **Pointers into the stack stop being stable.** Nothing holds one yet. Chapter
   24 gives every `CallFrame` a `slots` pointer into the stack, and at that
   point every frame has to be fixed up on every growth, or the stack has to stop
   moving. That is the decision this challenge defers, and it is why the book
   does not take it.

Setting a maximum is still worth doing and is not done here: unbounded growth
turns infinite recursion from a stack-overflow error into an out-of-memory kill.
Chapter 24 is where recursion exists and where that limit belongs.

## Challenge 4 — negating in place, measured

The book writes `push(-pop())`, which decrements `stackTop` and increments it
back for a value that never moves. In place is `vm.stackTop[-1] = -vm.stackTop[-1]`.

The challenge says "see if you can measure a performance difference", so both are
built and timed rather than reasoned about. `make bench` compiles the VM twice at
`-O3` and runs a chunk that is one constant, a thousand `OP_NEGATE`s and a
return, five hundred thousand times — five hundred million negates per variant,
best of five runs.

On an Apple M5, Apple clang 21:

```
negate in place    0.352 s  0.70 ns per OP_NEGATE  (500000000 executed)
push(-pop())       0.399 s  0.80 ns per OP_NEGATE  (500000000 executed)
```

So: yes, measurable, and about 12% — on a program that is 99.9% `OP_NEGATE`.
That last clause is the finding. A real Lox program has perhaps one negation in
fifty instructions, which makes the whole-program effect on the order of 0.2%,
and no benchmark in this repository could resolve that from noise.

Both halves matter. The optimisation is real — the store to `stackTop` is not
free even though it is to the hottest cache line in the process, because the
increment and the decrement are a dependency chain through the same register
that the next instruction's fetch has to wait on. And it is also irrelevant,
because the thing it speeds up is 2% of the instruction stream.

The same optimisation applies to any instruction whose stack height is unchanged.
Today that is only `OP_NEGATE`. Chapter 18 adds `OP_NOT`, which is the same
shape, and the binary operators are the bigger version of the same idea: `OP_ADD`
pops twice and pushes once, so it could write through `stackTop[-2]` and
decrement once instead of moving `stackTop` three times.

## Verification

```sh
make -C clox test                      # golden stdout + trace diff, allocator tests
make -C clox test HEAP=own             # identical stdout, our allocator
make -C clox test MODE=release
make -C clox test MODE=release HEAP=own
make -C clox asan                      # AddressSanitizer + UBSan
make -C clox asan HEAP=own
make -C clox bench                     # challenge 4, both ways
```

The golden test now pins two streams instead of one.

**stdout** — [`clox/test/expected.txt`](../clox/test/expected.txt) — is the
disassembly of four chunks and what running each of them printed. It must be
byte for byte identical in all six configurations. The four chunks are chosen so
that each covers something the others cannot: the book's own demo, chapter 14's
two challenges plus `OP_CONSTANT_LONG` through the dispatch loop, section 15.3's
`-((1.2 + 3.4) / 5.6)`, and 300 pushes to force the stack to grow.

**stderr** — [`clox/test/expected-trace.txt`](../clox/test/expected-trace.txt) —
is the execution trace, and it is checked differently per build. A debug build
diffs it. A release build asserts the file is *empty*, which is the stronger
claim: it says the trace is genuinely compiled out rather than merely quiet.

What none of this covers is the stack growth path under a moving allocator.
`make asan HEAP=own` is what covers that: `heap.c` is far more willing to move a
block on realloc than libc is, and AddressSanitizer is what turns a stale
`stackTop` from a wrong answer into a report.

The Go implementation is untouched and must stay green:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
./tool/booktest.sh
```

## Scope boundary

All four challenges are implemented.

[Chapter 14's scope boundary](14-chunks-of-bytecode.md#scope-boundary) predicted
two things about this chapter. One held and one did not.

It held for `disassembleInstruction`: it is now called from inside the
interpreter loop with a running instruction pointer rather than a loop variable,
and `(int)(vm.ip - vm.chunk->code)` is the arithmetic that has to agree with the
offset the disassembler computes for itself. It does, and the trace golden file
is what says so.

It missed on `getLine`. Chapter 14 expected the linear walk over line runs to
land on the error path. Chapter 15 has no error path — every value is a `double`
and no operation on two doubles can fail — so `getLine`'s only caller is still
the disassembler. It is now called once per instruction in a traced run, which
is far hotter than an error path would ever be, and still does not matter,
because a traced run is already spending an `fprintf` per instruction. Chapter
18 is where the error path arrives.

`Value` being a bare `double` also survived, for the same reason: nothing on the
stack can be the wrong type when there is only one type. That is the whole of
chapter 18, and it is the next thing to land after the scanner.

Chapter 16 adds a scanner and a REPL. The pressure lands in two places. `main.c`
stops being a demo and becomes a program that reads source text, so the four
hand-built chunks need somewhere else to live if the golden test is to survive.
And `interpret` stops taking a `Chunk*` and starts taking a `const char*`, which
is the point at which the VM stops being something you drive directly and starts
being the back end of something.
