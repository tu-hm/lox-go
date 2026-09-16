# Chapter 15 learning plan — A virtual machine

This plan follows
[Crafting Interpreters, Chapter 15](https://craftinginterpreters.com/a-virtual-machine.html)
through this repository's C implementation. Chapter 14 built a chunk and a
disassembler and ran nothing; this is the chapter where bytes start executing.

## Learning outcomes

By the end, you should be able to:

1. Explain why `READ_BYTE()` advances the instruction pointer as a side effect,
   and what that means for who consumes an operand.
2. Say why `ip` is a pointer rather than an index, and name the two things that
   invalidate it.
3. Explain why `stackTop` points at the next free slot rather than at the top
   value, and what `stackTop[-1]` means.
4. Say which operand `BINARY_OP` pops first and why, and name two expressions
   that would not catch the mistake.
5. Explain why `BINARY_OP` is wrapped in `do { } while (false)`.
6. Say why decoding a three-byte operand in one expression is a bug even though
   it compiles and usually works.
7. Explain what a reallocating stack breaks, and why the failure is silent.
8. Say what the trace costs, in the order of growth, and why it goes to stderr.
9. Name what chapter 15 cannot get wrong, and which chapter changes that.

The C implementation needs no toolchain setup — the Makefile uses `cc` and the
book's own flags:

```sh
make -C clox test
```

For the Go side, use this command prefix in the current environment:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache
```

## Session 1 — Eight opcodes and a `switch`

Read §15.1, then build and run:

```sh
make -C clox
./clox/build/debug-libc/clox
```

Four chunks run. The first is chapter 14's demo, which now prints `1.2` under
its own disassembly.

Now look at the trace, which is the same run with the stack shown:

```sh
./clox/build/debug-libc/clox > /dev/null
```

Read [`clox/vm.c`](../clox/vm.c)'s `run` alongside it. Before reading further,
answer from the code alone:

- The `for (;;)` has no increment and no condition. What ends it?
- `READ_BYTE()` appears twice in the `OP_CONSTANT` case — once directly and once
  inside `READ_CONSTANT()`. How many bytes does that case consume?
- There is no `default:` in the book's version. What happens to a byte that is
  not an opcode?

Then read [`clox/common.h`](../clox/common.h) and say why running the binary
normally shows no trace at all, and what the redirection above is doing.

Checkpoint: the disassembler and the VM both walk the same bytes and both have
to know how wide every instruction is. Neither calls the other. Write down what
keeps their two answers in agreement, and then find the place in
[`clox/test/expected.txt`](../clox/test/expected.txt) where a disagreement would
be visible.

## Session 2 — The stack has no bottom

Read §15.2, and `push`, `pop` and `resetStack` in [`clox/vm.c`](../clox/vm.c).

`stackTop` points at the next *free* slot. Predict, before checking against the
trace:

- What is `stackTop` when the stack is empty?
- How many comparisons does `push` do in the book's version? In this one?
- The trace line before `OP_RETURN` in the second chunk shows four values. How
  many does `OP_RETURN` remove?

That last one is the interesting one. Look at the `== long constants ==` chunk:
it pushes `1`, `2`, `3` and `4.5`, and `OP_RETURN` prints `4.5`. Three values are
still on the stack when the chunk ends.

Now break the cleanup. In `interpret`, comment out the reset:

```c
  // resetStack();

  return run();
```

Run `make -C clox test`. **The stdout golden still passes.** Only the trace
diff fails:

```
 0010    3 OP_RETURN
-          
+          [ 1 ][ 2 ][ 3 ]
 0000    1 OP_CONSTANT         0 '1.2'
-          [ 1.2 ]
+          [ 1 ][ 2 ][ 3 ][ 1.2 ]
```

Every printed answer is still correct, because each chunk only ever reads what it
pushed. The leak is real and entirely invisible to the program's output. Put it
back.

Checkpoint: that is the case for pinning the trace as well as the result, and it
is also an argument against — the trace file has to be regenerated every time an
opcode is added. Decide which side you are on, then say what a *third* kind of
check would have caught this without either file: something the VM could assert
about itself at the end of `interpret`.

## Session 3 — Which operand is on top

Read §15.3 and the `BINARY_OP` macro.

Work out on paper, from the bytecode in
[the chapter notes](15-a-virtual-machine.md#challenge-1--what-the-compiler-will-emit),
which operand of `3 - 2` is deeper on the stack. Then swap the two lines:

```c
#define BINARY_OP(op) \
    do { \
      double a = pop(); \
      double b = pop(); \
      push(a op b); \
    } while (false)
```

Predict the golden diff before running `make -C clox test`. It is one line:

```
-0.821429
+-1.21739
```

Two things to sit with. The `== deep stack ==` chunk still prints `45150`,
because it is three hundred additions and addition does not care. And the
disassembly above the changed line is byte for byte identical, because nothing
about the *program* changed — only what the machine did with it.

Check your own understanding against a case the test does not cover: `3 - 2 - 1`
evaluates to `2` under the swapped macro, and `1 + 2 * 3` still evaluates to `7`.

Now delete the `do { } while (false)` and leave the braces. It still compiles and
the tests still pass. Find the C that would stop compiling, write it, and confirm.
Then put both back.

Checkpoint: the swapped version is wrong for two of the four operators and right
for the other two. Say which test in `test/` would have caught it, and then say
why no test in this repository runs against the C implementation yet.

## Session 4 — One stream, two decoders

Read `OP_CONSTANT_LONG` in [`clox/vm.c`](../clox/vm.c) and
`constantLongInstruction` in [`clox/debug.c`](../clox/debug.c). Chapter 14's
challenge 2 made the operand three bytes; there are now two pieces of code that
know that.

Break one of them. Drop the third byte in the VM only:

```c
        int constant = READ_BYTE();
        constant |= READ_BYTE() << 8;
        // constant |= READ_BYTE() << 16;
```

Predict what happens before running it. Specifically: does the wrong *constant*
get loaded?

```sh
make -C clox test
```

```
 0006    | OP_CONSTANT_LONG  303 '4.5'
 0010    3 OP_RETURN
 -- 11 code bytes, 304 constants, 3 line runs
-4.5
 
 == deep stack ==
-45150
+257
```

No. The constant is exactly right — `303` fits in two bytes — and the chunk still
fails, because the byte that was not consumed is read as the next opcode. The
line that vanished is the one `OP_RETURN` should have printed; `OP_RETURN` was
consumed as a constant index instead.

Now read the trace for that chunk:

```
0006    | OP_CONSTANT_LONG  303 '4.5'
          [ 1 ][ 2 ][ 3 ][ 4.5 ]
0009    | OP_CONSTANT         7 '4'
          [ 1 ][ 2 ][ 3 ][ 4.5 ][ 4 ]
0011   -1 OP_CONSTANT         0 '1'
```

`0011   -1`. That is chapter 14's line encoding reporting an offset past the end
of the chunk, exactly as [`clox/line.h`](../clox/line.h) said it would: "`-1` is
here so that a bug shows up as an impossible line number instead of as a
plausible one." Go and read that comment now that it has paid.

The run ends with `clox: unknown opcode 87` on stderr — the `default:` case,
which the book does not have. Ask what the book's VM would have done here, and
then check: does `make -C clox asan` report anything? Work out why not before
looking at `count` and `capacity` in [`clox/chunk.h`](../clox/chunk.h).

Put it back.

Checkpoint: the chapter notes argue that writing this decode as a single
expression — `READ_BYTE() | (READ_BYTE() << 8) | (READ_BYTE() << 16)` — is a bug.
Try it. It will almost certainly pass. Say why passing is not evidence, and name
the build setting most likely to change the answer.

## Session 5 — A stack that moves

Read challenge 3's implementation: `growStack` in [`clox/vm.c`](../clox/vm.c),
and `GROW_CAPACITY` in [`clox/memory.h`](../clox/memory.h).

The demo's fourth chunk pushes 300 values, so the stack reallocates once. Predict
the number printed at the end of `== deep stack ==` before you look.

Now write the version a careless implementer writes — grow the buffer and stop
there:

```c
static void growStack(void) {
  int oldCapacity = vm.stackCapacity;
  vm.stackCapacity = oldCapacity < STACK_INITIAL ? STACK_INITIAL
                                                 : GROW_CAPACITY(oldCapacity);
  vm.stack = GROW_ARRAY(Value, vm.stack, oldCapacity, vm.stackCapacity);
}
```

(If you try to do this by deleting only the last line, `-Werror` stops you:
`height` becomes unused. That is worth noticing on its own — the compiler can see
that a value was computed and thrown away, and cannot see anything else that is
wrong here.)

Run it:

```sh
make -C clox test
```

The answer is wrong and it is wrong differently on different runs — `12254`, or
binary garbage, or the right answer. Nothing crashes. Run it three times.

Then:

```sh
make -C clox asan
```

```
ERROR: AddressSanitizer: heap-buffer-overflow
WRITE of size 8 at 0x61d000000880 thread T0
    #0 in push vm.c:67
freed by thread T0 here:
    #2 in growStack vm.c:35
```

Two stack traces: where it was written, and where the memory it was written to
stopped being valid. That pair is the whole report. Put the line back.

Checkpoint: the bug is a pointer into a buffer that moved. `vm.ip` is also a
pointer into a buffer, and nothing in this chapter can move *it*. Name the
chapter that can, and say which of the two hazards the book chose to avoid by
giving the stack a fixed size.

## Session 6 — Measuring challenge 4

Read the `OP_NEGATE` case, then run:

```sh
make -C clox bench
```

```
negate in place    0.352 s  0.70 ns per OP_NEGATE  (500000000 executed)
push(-pop())       0.399 s  0.80 ns per OP_NEGATE  (500000000 executed)
```

Before interpreting that, read [`clox/test/bench_vm.c`](../clox/test/bench_vm.c)
and answer:

- Why does it take the best of five runs rather than the mean?
- Why is there a warm-up run at all, for code with no JIT and no cache to fill?
- Why does the benchmark send stdout to `/dev/null`, and what would the number
  be measuring if it did not?
- The chunk is one constant, a thousand `OP_NEGATE`s and a return. What fraction
  of a *real* program's instructions are negations? What does that do to the 12%?

Then run it three more times and look at the spread. One of the two variants will
occasionally come back twice as slow. Decide whether that changes the conclusion.

Now do the experiment the challenge really asks for. Apply the same in-place
trick to `OP_ADD`: it pops two and pushes one, so it can write through
`vm.stackTop[-2]` and decrement `stackTop` once instead of moving it three times.
Change `BINARY_OP`, add a chunk of a thousand additions to the benchmark, and
measure.

Checkpoint: chapter 14's learning plan ended by asking what a benchmark here
could possibly measure, given that nothing executed. You have now answered it.
Say what the *next* useful measurement is, and why it cannot be taken until
chapter 17 — and then find the Go numbers in
[`interpreter/bench_test.go`](../interpreter/bench_test.go) that it will be
compared against.

## Session 7 — What cannot go wrong yet

Run everything:

```sh
make -C clox test
make -C clox test HEAP=own
make -C clox test MODE=release
make -C clox test MODE=release HEAP=own
make -C clox asan
make -C clox asan HEAP=own
```

All six print the same golden line, and stdout is byte for byte identical in all
six. The release build additionally asserts that stderr is *empty*, which is a
stronger claim than diffing it: it says the trace is compiled out, not merely
quiet. Find the two lines of [`clox/Makefile`](../clox/Makefile) that do that.

Then confirm the Go side is untouched:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
./tool/booktest.sh
```

Now the gap. Write down every way this VM could report an error to a user. The
list is one item long and it is `unknown opcode`, which no chunk this program
builds can produce. Then work out why: go through the six opcodes and find an
input to any of them that is invalid. `1 / 0` is not one — try it.

Final checkpoint: `Value` is still a bare `double`, and that is the reason the
list above is empty. Say what chapter 18 adds to `Value`, what the first runtime
error will be, and which function written in chapter 14 has been waiting for it
since before there was anything to run.

## Challenges

All four are implemented. Two are answers on paper and two are code.

1. **Implemented, as prose** — the bytecode for the four expressions. See
   [the chapter notes](15-a-virtual-machine.md#challenge-1--what-the-compiler-will-emit).
   The part worth doing yourself is the observation at the end: a stack machine's
   instruction sequence is the postorder traversal of the syntax tree, which
   means [`ast/rpn.go`](../ast/rpn.go) — chapter 5's challenge, written months
   ago in Go — has been emitting this bytecode all along with different names for
   the opcodes:

   ```
   $ go run . -print=rpn
   > print 1 + 2 * 3 - 4 / -5;
   (print 1 2 3 * + 4 5 negate / -)
   ```

   Line that up against the opcodes and find the one token in it that is not an
   instruction.

2. **Implemented, as prose** — the minimal instruction set. See
   [the chapter notes](15-a-virtual-machine.md#challenge-2--a-minimal-instruction-set).
   Push the question further than the book does: the enum has 248 unused opcode
   values. If opcode space is free, what stops you from adding a specialised
   instruction for every common pair — `OP_ADD_CONSTANT`, `OP_NEGATE_ADD`? Work
   out what a superinstruction costs that an ordinary one does not, and then
   count how many of them a compiler would have to know about.

3. **Implemented** — a stack that grows. See
   [the chapter notes](15-a-virtual-machine.md#challenge-3--a-stack-that-grows),
   and session 5 for what it costs when done wrong.

   The extension it is deliberately short of is a *limit*. Unbounded growth
   turns infinite recursion from a stack-overflow error into an out-of-memory
   kill of the whole process, which is strictly worse. There is no recursion
   until chapter 24, which is the argument for waiting; the argument against is
   that `test/limit/stack_overflow.lox` already exists in the corpus and the Go
   implementation already passes it.

4. **Implemented, and measured** — negating in place. See
   [the chapter notes](15-a-virtual-machine.md#challenge-4--negating-in-place-measured).
   Both versions are in the source, behind `CLOX_NEGATE_VIA_STACK`, because
   "see if you can measure a performance difference" is not a question to answer
   by reading.
