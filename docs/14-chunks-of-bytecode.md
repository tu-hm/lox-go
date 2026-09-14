# Chapter 14 — Chunks of bytecode, in C

Implementation of
[Chunks of Bytecode](https://craftinginterpreters.com/chunks-of-bytecode.html),
the first chapter of part III. Chapter 13 finished the tree-walking interpreter;
this starts the second implementation, and it is written in C rather than Go.

> Status: implemented, including all four challenges. A `Chunk` holds bytecode,
> a constant pool, and run-length encoded line information; a disassembler
> renders it. `OP_CONSTANT_LONG` carries a 24-bit operand, and `reallocate()`
> can be served either by libc or by a boundary-tag allocator over a single
> up-front `malloc`. Nothing executes yet — chapter 15 adds the VM.

For a guided walkthrough, follow the [Chapter 14 learning plan](14-learning-plan.md).

## Why this one is in C

The Go implementation is not going away, and this is not a rewrite of it. The
two sit side by side, as jlox and clox do in the book's own repository.

The reason for the language change is specific rather than stylistic. Section
14.3 is three pages about `reallocate`, `GROW_CAPACITY` and `GROW_ARRAY`, and Go
deletes all of it: `append` already grows by doubling, already amortises, and
already frees. A Go port would be `c.code = append(c.code, byte)` and a comment
explaining what the chapter would have taught. That is a fine trade for chapters
whose subject is a language feature — part II made it a dozen times — but this
chapter's subject *is* the memory, and the same trade here leaves nothing.

It also decides challenge 4. "Implement `reallocate()` on top of one `malloc`"
is a real exercise in C and a fiction under a garbage collector; in Go it would
have meant hand-rolling an arena over a byte slice to imitate a problem the
runtime does not have.

What the repository gives up is a shared test corpus — `tool/booktest.sh` runs
`.lox` programs, and chapter 14 runs nothing. That returns in chapter 16.

## What the chapter adds

| Type | Shape | What it is |
|---|---|---|
| `Value` | `double` | one constant; a tagged union in chapter 18 |
| `ValueArray` | `capacity`, `count`, `Value*` | the constant pool |
| `LineRun` | `line`, `count` | challenge 1 — one source line, and how many bytes came from it |
| `LineArray` | `capacity`, `count`, `LineRun*` | the runs, in code order |
| `Chunk` | `code`, `lines`, `constants` | a compiled unit |

| Opcode | Operand | Meaning |
|---|---|---|
| `OP_CONSTANT` | 1 byte | load `constants[operand]` |
| `OP_CONSTANT_LONG` | 3 bytes, little-endian | challenge 2 — the same, past index 255 |
| `OP_RETURN` | none | return from the current function |

## One function allocates

Every allocation in the program goes through
[`reallocate`](../clox/memory.c), which handles all four operations at once —
allocate, free, grow, shrink — selected by comparing `oldSize` and `newSize`
against zero. `GROW_ARRAY` and `FREE_ARRAY` are macros that compute byte counts
and call it; they do no work of their own.

That looks like indirection for its own sake in a chapter with three data
structures. It is the load-bearing decision of part III. Chapter 26 needs to
know the total number of live bytes to decide when to collect, and a single
choke point is how it will find out without instrumenting every call site.

Challenge 4 is the early proof. `heap.c` replaces libc's allocator entirely, and
the integration is one `#ifdef` inside `reallocate`:

```c
#ifdef CLOX_OWN_HEAP
  void* result = heapRealloc(pointer, newSize);
#else
  void* result = realloc(pointer, newSize);
#endif
```

No other file mentions `heap.h`. `make test HEAP=own` has to produce output
identical to `make test` byte for byte, and does.

## A constant is an index, not a value

Instructions do not carry their operands' values, because a `double` is eight
bytes and an index is one. The bytecode holds `OP_CONSTANT 0`; the pool holds
`1.2`.

Keeping `Value` a bare `double` rather than reusing the Go side's `any` is
deliberate. It makes the pool a flat block of memory — 304 constants are 2432
contiguous bytes, walked in a handful of cache lines rather than chased through
304 pointers — and that locality is most of what part III is claiming it can
win. Chapter 18 pays for booleans and nil with a tagged union, and the size of
that diff is the measure of how well this is contained.

## Lines are the only thing a chunk does not need

Everything else in a `Chunk` is read while the program runs. Line numbers are
read only when it fails.

The chapter stores them in an array parallel to the code — byte `i` came from
`lines[i]` — which is four bytes per bytecode byte and almost entirely
redundant, since bytecode is generated in source order and a single statement
compiles to a run of bytes that all carry the same number.

Challenge 1 replaces it with runs. Lookup stops being an index and becomes a
walk, which is the trade the challenge exists to make you name: it is only
acceptable because of *when* the data is read. A `getLine` that costs a loop is
free in a process that is about to report an error and stop.

The break-even is exact. A chunk of `n` code bytes drawn from `L` source lines
costs `8L` bytes as runs against `4n` as a parallel array, so runs win whenever
`n > 2L` — more than two bytes of bytecode per source line. Even `1 + 2;`
clears that.

## The disassembler cannot loop

`disassembleInstruction` returns the offset of the *next* instruction instead of
taking a fixed stride, because only it knows how wide the instruction it just
decoded was: `OP_RETURN` is one byte, `OP_CONSTANT` is two, `OP_CONSTANT_LONG`
is four.

That return value is why challenge 2 is worth doing here rather than later.
Without a second operand width, the mechanism exists but is never exercised, and
a disassembler that is wrong about instruction widths is wrong about every
instruction after the first one.

An unknown opcode prints and advances by one rather than aborting. A
disassembler is at its most useful when the chunk is malformed, so it must
survive reading garbage.

## Challenge 1 — run-length encoded lines

[`clox/line.c`](../clox/line.c). `writeLineArray` extends the last run when the
line repeats and appends one when it does not; `lineArrayGet` walks runs
subtracting counts. Out-of-range offsets return `-1` rather than the nearest
line, so a bug surfaces as an impossible number instead of a plausible one.

The walk is linear, not a binary search, even though the runs are sorted and a
search would work. `n` is the number of *lines*, this runs once per error, and
the array is already in the right shape to search the day a profile says so.

## Challenge 2 — `OP_CONSTANT_LONG`

[`writeConstant`](../clox/chunk.c) adds the constant and then picks the
instruction by index. The operand is three bytes, least significant first —
big-endian would read better in a hex dump, but little-endian matches the host,
so chapter 15's dispatch loop can eventually take one unaligned load instead of
three shifts.

Past 16.7 million constants in one chunk the encoding runs out. That is reported
and fatal, because chapter 14 has no compiler and so nowhere to attribute a
source location; chapter 17 turns it into an ordinary compile error.

## Challenge 3 — what a real `malloc` does

`heap.c` is roughly where allocator design stood in the early 1990s. Three
production allocators, and what each does that it does not:

**glibc (ptmalloc2)** segregates free blocks into *bins* by size — 62 small bins
holding one size class each, larger bins holding ranges and kept sorted — so a
fit is found by indexing rather than by scanning. In front of the bins sits a
per-thread *tcache* of single-linked lists, so the common small allocation
touches no lock at all. Multiple *arenas* let threads allocate in parallel.
Blocks still carry boundary tags, and the coalescing logic is recognisably the
same as here.

**musl (mallocng)** keeps metadata *out of band*. Where `heap.c` writes a header
immediately before memory it hands to the caller, mallocng groups same-size
slots together and stores their bookkeeping elsewhere, with the slot's index
encoded in a small offset byte. A buffer overrun therefore corrupts another
allocation rather than the allocator's own view of the heap, which turns a large
class of heap-corruption exploits into crashes.

**macOS (libmalloc)**, which is what the libc build here actually uses, splits by
size into a *nano* zone for allocations under 256 bytes, a *tiny/small* magazine
allocator with per-CPU magazines, and `mmap` direct for large ones. Per-CPU
rather than per-thread means no lock on the fast path without an arena per
thread.

The three themes `heap.c` is missing are the same three: **size-class
segregation** so fits are indexed rather than searched, **thread or CPU
locality** so the fast path is lock-free, and **out-of-band or hardened
metadata** so an overrun is not immediately an exploit. What it does have — the
boundary tag — all three kept in some form, because the problem it solves is
unavoidable: a freed block must find its left-hand neighbour, and there is no
back pointer.

## Challenge 4 — the allocator

[`clox/heap.c`](../clox/heap.c). One `malloc` of `CLOX_HEAP_SIZE` (1 MiB by
default) at first use, and nothing after it.

```
+--------+----------------------------+--------+
| header |          payload           | footer |
|   8    |        size - 16           |   8    |
+--------+----------------------------+--------+
             ^ what the caller gets
```

Header and footer hold the same word: total block size with the low bit set when
in use. Sizes are multiples of 16, so the low four bits are free to borrow.

**The footer is the whole design.** Merging a freed block with the block to its
*right* is easy — add this header's size and you land on it. Merging with the
block to its *left* has no pointer to follow, and walking from the start of the
arena would make every `free` O(n). A trailing copy of the size fixes it,
because the word immediately before any header is the previous block's footer.
Eight bytes per block buys O(1) coalescing in both directions.

The rest is conventional: an explicit doubly-linked free list threaded through
the payloads of free blocks, so the list costs no memory; first fit with
splitting when the remainder could hold a block of its own; and prologue and
epilogue sentinel blocks at the arena's ends, both permanently marked allocated,
so `coalesce` reads its neighbours without ever checking whether they exist.

One departure from the textbook version, aimed at this program specifically:
`heapRealloc` grows in place by absorbing the next block when it is free.
`GROW_ARRAY` doubles the same chunk repeatedly, and the block being doubled is
usually the most recent allocation with nothing but free space behind it —
without the in-place case, every growth copies.

Exhaustion returns `NULL` and leaves the heap usable, which
[`clox/test/test_heap.c`](../clox/test/test_heap.c) checks: a failed allocation
must not leave the free list half-updated. The same goes for a request that
cannot be rounded up without wrapping — unguarded, `heapAlloc(SIZE_MAX)` asks
for a 32-byte block, finds one, and hands back a pointer to it.

Owning the allocator also buys an exact leak check. LeakSanitizer is unavailable
on macOS, but `heapStats().allocatedBlocks` is authoritative, so the `HEAP=own`
build asserts at exit that nothing survived `freeChunk`. It reports on stderr so
that stdout — and therefore the golden test — is unaffected.

## Verification

The golden test diffs the demo binary's disassembly against
[`clox/test/expected.txt`](../clox/test/expected.txt), which pins the book's own
three-line output exactly, plus a second chunk that fires `OP_CONSTANT_LONG` and
spans three source lines so both challenges are observable. `test_heap.c` covers
what printed output cannot see: alignment across every size from 1 to 256,
splitting, coalescing left, right and both at once, block reuse, in-place and
copying `realloc`, shrink-and-release, the null and zero conventions,
exhaustion, and a 4000-operation churn that re-checks every invariant
throughout.

```sh
make -C clox test                     # golden diff + allocator unit tests
make -C clox test HEAP=own            # identical output, our allocator
make -C clox test MODE=release
make -C clox test MODE=release HEAP=own
make -C clox asan                     # AddressSanitizer + UBSan
make -C clox asan HEAP=own
```

`heapCheck()` walks the implicit list of all blocks and the explicit list of free
ones and reports any disagreement — mismatched header and footer, a block off a
16-byte boundary, two free blocks left adjacent, an allocated block on the free
list, a cycle, or sizes that do not sum to the arena.

The Go implementation is untouched and must stay green:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
```

## Scope boundary

All four challenges are implemented, challenge 3 as the prose above rather than
as code.

Two things here are built for a chapter that has not arrived. `getLine` is on no
hot path today because nothing runs; chapter 15 puts it on the error path, and
that is the first time its linear walk is a claim rather than an assumption.
And `reallocate`'s `oldSize` parameter is unused by both allocators — libc's
`realloc` knows the size, and `heap.c` reads it from the header. Chapter 26 is
what reads it, to keep a running total of live bytes.

Chapter 15 adds the VM: an `ip`, a value stack, and a dispatch loop. The
pressure should land on two places. `disassembleInstruction` becomes a debug
hook called from inside the interpreter loop rather than a standalone tool,
which is the first time its offset arithmetic has to agree with a *running*
instruction pointer instead of a loop variable. And `Value` being a bare
`double` stops being free the moment the stack needs to hold anything a
comparison could fail on.
