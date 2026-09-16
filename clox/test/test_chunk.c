// test_chunk is the demo that used to be main(). Chapter 16 turns main.c into a
// REPL and a file runner, and these four chunks are the only thing that can run
// bytecode at all until chapter 17 -- so they move here rather than being
// deleted, and clox/test/expected.txt goes on meaning what it meant.
//
// It is a test binary in the same sense test_heap.c is: it links the same
// objects the interpreter does, minus main.o.

#include <stdio.h>

#include "chunk.h"
#include "common.h"
#include "debug.h"
#include "vm.h"

#ifdef CLOX_OWN_HEAP
#include "heap.h"
#endif

// bookChunk is the chapter 14 demo, byte for byte. Its disassembly is the three
// lines printed at the end of section 14.6; the value under it is chapter 15
// running the same bytes.
static void bookChunk(void) {
  Chunk chunk;
  initChunk(&chunk);

  writeConstant(&chunk, 1.2, 123);
  writeChunk(&chunk, OP_RETURN, 123);

  disassembleChunk(stdout, &chunk, "test chunk");
  interpretChunk(&chunk);
  freeChunk(&chunk);
}

// constantChunk exercises what the book's demo cannot: three bytes on one line
// leave chapter 14's two challenges invisible. Running it also puts
// OP_CONSTANT_LONG through the dispatch loop, which is the first decoder of that
// operand other than the disassembler.
static void constantChunk(void) {
  Chunk chunk;
  initChunk(&chunk);

  writeConstant(&chunk, 1.0, 1);
  writeConstant(&chunk, 2.0, 1); // same line as the one above: prints as |
  writeConstant(&chunk, 3.0, 2);

  // Fill the pool past the one-byte operand limit without emitting any code,
  // so the constant written next is forced into the long form.
  for (int i = 0; i < 300; i++) {
    addConstant(&chunk, (Value)i);
  }

  writeConstant(&chunk, 4.5, 2);
  writeChunk(&chunk, OP_RETURN, 3);

  disassembleChunk(stdout, &chunk, "long constants");

  // The point of challenge 1, as a number. Eleven code bytes drawn from three
  // source lines cost three runs, not eleven ints.
  printf("-- %d code bytes, %d constants, %d line runs\n",
         chunk.count, chunk.constants.count, chunk.lines.count);

  // OP_RETURN pops one value and prints it; 1, 2 and 3 are still on the stack
  // when this returns. Nothing minds, and interpret() resets before the next.
  interpretChunk(&chunk);
  freeChunk(&chunk);
}

// arithmeticChunk is section 15.3's worked example, -((1.2 + 3.4) / 5.6), hand
// assembled. Chapter 17 is where a compiler produces these bytes from the text.
static void arithmeticChunk(void) {
  Chunk chunk;
  initChunk(&chunk);

  writeConstant(&chunk, 1.2, 1);
  writeConstant(&chunk, 3.4, 1);
  writeChunk(&chunk, OP_ADD, 1);
  writeConstant(&chunk, 5.6, 1);
  writeChunk(&chunk, OP_DIVIDE, 1);
  writeChunk(&chunk, OP_NEGATE, 1);
  writeChunk(&chunk, OP_RETURN, 1);

  disassembleChunk(stdout, &chunk, "arithmetic");
  interpretChunk(&chunk);
  freeChunk(&chunk);
}

// deepChunk is challenge 3: push more values than the book's fixed stack holds.
// Under the book's VM this is undefined behaviour with no diagnostic. Here the
// stack reallocates, and the number printed at the end is the proof.
static void deepChunk(void) {
  Chunk chunk;
  initChunk(&chunk);

  const int n = 300;
  for (int i = 1; i <= n; i++) {
    writeConstant(&chunk, (Value)i, 1);
  }
  for (int i = 1; i < n; i++) {
    writeChunk(&chunk, OP_ADD, 2);
  }
  writeChunk(&chunk, OP_RETURN, 3);

  // Neither disassembled nor traced. 988 code bytes is a page of output on its
  // own, and the trace would be far worse: it prints the whole stack before
  // every instruction, so tracing n pushes costs O(n^2) lines of text.
  vmSetTrace(NULL);

  printf("== deep stack ==\n");
  interpretChunk(&chunk);
  printf("-- %d code bytes, stack grew to %d slots\n",
         chunk.count, vmStackCapacity());

  freeChunk(&chunk);
}

int main(int argc, const char* argv[]) {
  initVM();

  // The trace goes to stderr so that stdout stays byte for byte identical across
  // every build configuration -- which is what `make test HEAP=own` and
  // `make test MODE=release` exist to check. Without DEBUG_TRACE_EXECUTION
  // compiled in, this call records the stream and nothing ever reads it.
  vmSetTrace(stderr);

  bookChunk();
  printf("\n");
  constantChunk();
  printf("\n");
  arithmeticChunk();
  printf("\n");
  deepChunk();

  freeVM();

#ifdef CLOX_OWN_HEAP
  // Owning the allocator makes a leak check exact rather than heuristic: every
  // chunk was freed and so was the stack, so nothing may still be live.
  // LeakSanitizer would be the usual way to ask, and it is unavailable on macOS.
  //
  // This writes to stderr, never stdout, so the golden output stays byte for
  // byte the same as the libc build's.
  HeapStats stats = heapStats();
  if (stats.allocatedBlocks != 0 || !heapCheck()) {
    fprintf(stderr, "clox: %d block(s) still live at exit\n",
            stats.allocatedBlocks);
    return 1;
  }
#endif
  return 0;
}
