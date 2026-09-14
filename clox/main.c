#include <stdio.h>

#include "chunk.h"
#include "common.h"
#include "debug.h"

#ifdef CLOX_OWN_HEAP
#include "heap.h"
#endif

// bookChunk is the chapter's own demo, byte for byte. Its output is the three
// lines printed at the end of section 14.6.
static void bookChunk(void) {
  Chunk chunk;
  initChunk(&chunk);

  writeConstant(&chunk, 1.2, 123);
  writeChunk(&chunk, OP_RETURN, 123);

  disassembleChunk(&chunk, "test chunk");
  freeChunk(&chunk);
}

// challengeChunk exercises what the book's demo cannot: three bytes on one line
// leave both challenges invisible.
static void challengeChunk(void) {
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

  disassembleChunk(&chunk, "long constants");

  // The point of challenge 1, as a number. Eleven code bytes drawn from three
  // source lines cost three runs, not eleven ints.
  printf("-- %d code bytes, %d constants, %d line runs\n",
         chunk.count, chunk.constants.count, chunk.lines.count);

  freeChunk(&chunk);
}

int main(int argc, const char* argv[]) {
  bookChunk();
  printf("\n");
  challengeChunk();

#ifdef CLOX_OWN_HEAP
  // Owning the allocator makes a leak check exact rather than heuristic: both
  // chunks were freed, so nothing may still be live. LeakSanitizer would be the
  // usual way to ask, and it is unavailable on macOS.
  //
  // This writes to stderr, never stdout, so the golden output stays byte for
  // byte the same as the libc build's -- which is the claim `make test
  // HEAP=own` exists to check.
  HeapStats stats = heapStats();
  if (stats.allocatedBlocks != 0 || !heapCheck()) {
    fprintf(stderr, "clox: %d block(s) still live at exit\n",
            stats.allocatedBlocks);
    return 1;
  }
#endif
  return 0;
}
