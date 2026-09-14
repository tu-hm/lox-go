# Chapter 14 learning plan — Chunks of bytecode

This plan follows
[Crafting Interpreters, Chapter 14](https://craftinginterpreters.com/chunks-of-bytecode.html)
through this repository's C implementation. It is the first chapter of part III:
a second interpreter starts here, and the tree-walker in Go stays exactly where
chapter 13 left it.

## Learning outcomes

By the end, you should be able to:

1. Explain what bytecode buys over walking a tree, and what it costs, without
   using the word "faster" on its own.
2. Say why a growth factor of two is not an arbitrary choice, and what writing
   *n* bytes costs under a growth factor of one.
3. Explain why instructions hold an index into a constant pool rather than the
   constant.
4. Say why `disassembleInstruction` returns the next offset instead of taking a
   step size, and what breaks first when it is wrong.
5. Explain why a line-number lookup is allowed to be slow when a constant
   lookup is not.
6. Count the break-even point for run-length encoding lines, in bytes.
7. Say why every allocation goes through one function, and name the chapter that
   collects on the debt.
8. Explain what a block's footer is for, and why removing it breaks `free` and
   not `malloc`.
9. Say what the golden test cannot see, and what covers it instead.

The C implementation needs no toolchain setup — the Makefile uses `cc` and the
book's own flags:

```sh
make -C clox test
```

For the Go side, use this command prefix in the current environment:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache
```

## Session 1 — What a chunk is

Read §14.1–14.3, then build and run:

```sh
make -C clox
./clox/build/debug-libc/clox
```

The first three lines are the book's own output, reproduced exactly. Read
[`clox/chunk.h`](../clox/chunk.h) and [`clox/main.c`](../clox/main.c) and say
which call produced each byte.

Before reading further, answer from the header alone: a `Chunk` has a `count`
and a `capacity` and they are different numbers. Which one does
`disassembleChunk` loop to, and what would go wrong if it used the other?

Then read §14.1 again with a specific question in mind. The chapter argues
against walking the AST on cache-locality grounds, and the tree-walker it is
arguing against is right there. [Chapter 13's scope
boundary](13-inheritance.md#scope-boundary) names the three costs it left behind:
a property read is a map lookup ([`interpreter/instance.go`](../interpreter/instance.go)),
a method access allocates to bind the receiver (`bind` in
[`interpreter/function.go`](../interpreter/function.go)), and a call allocates a
scope ([`interpreter/environment.go`](../interpreter/environment.go)). Read all
three, and say which a bytecode VM removes outright and which it only moves.

Checkpoint: bytecode is an array of bytes with no structure at all, and an AST is
a tree that knows what everything is. The bytecode version has strictly less
information in it. What has to hold the difference?

## Session 2 — A constant is an index

Read §14.5 and `addConstant` and `writeConstant` in
[`clox/chunk.c`](../clox/chunk.c).

Predict each, then check against the disassembly:

- How many bytes does `writeConstant(&chunk, 1.2, 123)` add to `code`?
- How many does it add to `constants`?
- What does `writeConstant(&chunk, 1.2, 1)` called twice produce — one pool
  entry or two?
- Why does `addConstant` return an `int` when the operand is a `uint8_t`?

The third one is worth sitting with. The pool does no deduplication, so a
program with a thousand `1` literals stores it a thousand times. Work out where
deduplication would have to go, and why it cannot live in `addConstant` as
written.

Checkpoint: `Value` is a bare `double`, so the pool is 8 bytes per entry with
nothing to follow. Name one thing chapter 18 adds that makes that untrue, and
say whether the pool stays an array.

## Session 3 — The disassembler cannot loop

Read §14.4 and [`clox/debug.c`](../clox/debug.c). Note that
`disassembleChunk`'s `for` has no increment: the body assigns `offset` from the
return value.

Work out, before running anything, what the disassembly of the second chunk
would look like if `constantLongInstruction` returned `offset + 2` instead of
`offset + 4`.

Then break challenge 2 on purpose. In `writeConstant`, force the short form:

```c
  if (1) {
    writeChunk(chunk, OP_CONSTANT, line);
    writeChunk(chunk, (uint8_t)constant, line);
    return;
  }
```

Run `make -C clox test`. The golden diff is:

```
-0006    | OP_CONSTANT_LONG  303 '4.5'
+0006    | OP_CONSTANT        47 '44'
```

Two things to notice. Nothing crashed, and the number printed is a perfectly
plausible constant — 303 truncated into a byte is 47, and the pool has an entry
there. Put it back.

Checkpoint: this is the failure mode a disassembler exists to catch, and it was
caught by a text diff rather than by any check inside the code. What would an
assertion inside `writeConstant` have had to compare?

## Session 4 — Lines, and when slow is free

Read §14.6, then [`clox/line.h`](../clox/line.h) — the comment, not just the
struct — and `lineArrayGet` in [`clox/line.c`](../clox/line.c).

The chapter stores one `int` per code byte. This stores runs. Do the arithmetic
yourself before reading the answer in
[the chapter notes](14-chunks-of-bytecode.md#lines-are-the-only-thing-a-chunk-does-not-need):
for a chunk of *n* code bytes drawn from *L* source lines, when is the run
encoding smaller? Then say why that inequality is satisfied by essentially every
real program.

Now break the decoder's boundary. In `lineArrayGet`, change the test:

```c
    if (offset <= 0) return array->runs[i].line;
```

Run `make -C clox test`:

```
-0004    2 OP_CONSTANT         2 '3'
+0004    | OP_CONSTANT         2 '3'
```

An off-by-one in the decoder does not produce a wrong-looking line number; it
produces a *missing* one, because the disassembler prints a bar whenever two
adjacent offsets agree. Work out which offsets now agree that should not. Put it
back.

Checkpoint: `lineArrayGet` returns `-1` past the end rather than the last line.
Nothing in the program can currently ask for an offset past the end. Why is the
`-1` there anyway, and what would the alternative hide?

## Session 5 — One function allocates

Read [`clox/memory.h`](../clox/memory.h) and [`clox/memory.c`](../clox/memory.c),
and find every caller of `GROW_ARRAY` (there are three).

Say which of the four operations — allocate, free, grow, shrink — each of these
performs, reading only the arguments:

```c
reallocate(NULL, 0, 64)
reallocate(p, 64, 0)
reallocate(p, 64, 128)
reallocate(p, 128, 64)
```

Now break the growth strategy. In `memory.h`:

```c
#define GROW_CAPACITY(capacity) ((capacity) + 1)
```

Run `make -C clox test`. **Everything passes.** That is the session.

Work out why no test noticed, then answer concretely: the demo writes 304
constants. How many times does `writeValueArray` call `reallocate` under
doubling, and how many under `+ 1`? Add a counter to `reallocate` and check your
arithmetic. Then put both back.

Checkpoint: a growth factor is a performance property, and this repository has
benchmarks for the Go interpreter
([`interpreter/bench_test.go`](../interpreter/bench_test.go)) and none for the C
one. What would a benchmark here have to measure, given that nothing executes?

## Session 6 — Writing `malloc`

Challenge 4. Read the block-layout comment at the top of
[`clox/heap.c`](../clox/heap.c), then `coalesce`, then `place`.

Draw the arena on paper after each step, with header and footer values:

```c
void* a = heapAlloc(64);
void* b = heapAlloc(64);
void* c = heapAlloc(64);
heapFree(a);
heapFree(c);
heapFree(b);
```

Predict `heapStats().freeBlocks` after each `heapFree`, then check against
`freeingCoalescesWithBothNeighbours` in
[`clox/test/test_heap.c`](../clox/test/test_heap.c). One of the three answers
surprises most people.

Now remove the footer. In `place`, delete the second line:

```c
  if (csize - asize >= MIN_BLOCK) {
    PUT(HDRP(bp), PACK(asize, 1));
    PUT(FTRP(bp), PACK(asize, 1));   // <-- delete this
```

Run `make -C clox test HEAP=own`:

```
  FAIL allocationSplitsRatherThanTakingTheWholeBlock: heapCheck()
  FAIL freeingCoalescesWithTheNextBlock: heapCheck()
  FAIL freeingCoalescesWithThePreviousBlock: heapCheck()
  FAIL freeingCoalescesWithBothNeighbours: heapStats().freeBlocks == 2
make: *** [test] Segmentation fault: 11
```

`heapCheck` catches it on the first allocation that splits, four tests before
anything crashes — and the run dies anyway. Sit with that ordering before
reading on: an invariant check told you the heap was already broken and did not
stop it from getting worse.

Now find where it dies:

```sh
make -C clox asan HEAP=own
```

```
#0 coalesce heap.c:134
```

Line 134 is `GET_ALLOC(FTRP(PREV_BLKP(bp)))`. Say why the *allocation* path
dropping a write breaks the *free* path, and why it broke `PREV_BLKP` and not
`NEXT_BLKP`. Put it back.

Checkpoint: this reported five failures and then crashed. An assertion that
aborted on the first one would have given a shorter, more precise report and
strictly less information. Which do you want here — and does the answer change
in chapter 15, when the thing allocating is a VM in a dispatch loop rather than
a test?

## Session 7 — Two implementations, and what nothing tests

Run every configuration:

```sh
make -C clox test
make -C clox test HEAP=own
make -C clox test MODE=release
make -C clox test MODE=release HEAP=own
make -C clox asan
```

All five print the same golden line. The `HEAP=own` one is the interesting
claim: the program cannot tell which allocator it is running on, which is only
true because `reallocate` is the single choke point from session 5.

Then confirm the Go side is genuinely untouched:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
env GOTOOLCHAIN=go1.26.6 go run . examples/inheritance.lox
```

Now the gap. `tool/booktest.sh` runs 265 `.lox` programs against the Go binary,
and none of them can run against this one, because nothing here executes. List
what *is* covered for the C implementation and by what, and then write down the
first chapter at which the two implementations could be compared on the same
input.

Final checkpoint: the golden test pins printed output and `test_heap.c` pins the
allocator's invariants. Between them they miss an entire category of bug that
chapter 15 will introduce on its first day. Which category, and what kind of test
catches it?

## Challenges

All four are implemented. The first two are code in the chapter; the third is
prose; the fourth is most of a week if you write it yourself.

1. **Implemented** — run-length encoded lines. See
   [the chapter notes](14-chunks-of-bytecode.md#challenge-1--run-length-encoded-lines).
   The decision worth arguing with is the linear walk: the runs are sorted, so a
   binary search is available and was not taken. Session 4's checkpoint is the
   case for it; the case against is that `L` is small and the loop is the kind a
   branch predictor eats.

2. **Implemented** — `OP_CONSTANT_LONG`. See
   [the chapter notes](14-chunks-of-bytecode.md#challenge-2--op_constant_long).
   The book asks you to weigh the complexity against the memory saved. Weigh it
   in the other direction too: what would a *single* three-byte instruction cost,
   with no short form at all? Count the bytes for a realistic function, and then
   say which of the two answers a JIT would prefer.

3. **Implemented, as prose** — see
   [the chapter notes](14-chunks-of-bytecode.md#challenge-3--what-a-real-malloc-does)
   for glibc, musl and libmalloc. The exercise is to read one of them. Start with
   musl's `mallocng`, which is the smallest by a wide margin and the only one you
   can hold in your head in an afternoon.

4. **Implemented** — a boundary-tag allocator with an explicit free list. See
   [the chapter notes](14-chunks-of-bytecode.md#challenge-4--the-allocator).

   Two extensions it is deliberately short of, in increasing order of payoff:
   *segregated fits* (bin free blocks by size class, so `findFit` indexes instead
   of scanning — this is the single biggest difference between `heap.c` and
   ptmalloc), and *address-ordered free lists* (insert in address order rather
   than at the head, which measurably reduces fragmentation and costs a walk on
   every free). `heapStats().largestFree` against `freeBytes` is already the
   fragmentation number to measure them with.

One extension none of the challenges asks for, and the right time for it is now
rather than after chapter 15: write down what the tree-walker costs. Chapter
13's plan suggested the same thing and it is the only way part III's claim gets
tested against something other than a feeling. The benchmarks in
[`interpreter/bench_test.go`](../interpreter/bench_test.go) are the baseline; the
VM lands next chapter, and the first honest comparison is possible in chapter 17.
