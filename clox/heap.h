#ifndef clox_heap_h
#define clox_heap_h

#include "common.h"

// heap.c is challenge 4: reallocate() served from a single up-front malloc
// instead of from libc, one call at startup and none after it.
//
// It is a real allocator, not an arena that never frees -- the interpreter
// shrinks arrays as well as growing them, and chapter 26's collector will free
// in bulk, so a bump pointer would run the process out of memory on a program
// that merely churns. What it owes, then, is the two hard parts of malloc:
// finding a block of the right size, and putting a freed block back in a state
// where it can be found again next to its neighbours rather than instead of
// them.
//
// Nothing outside memory.c calls any of this. The one #ifdef in reallocate() is
// the entire integration, which is the structural claim chapter 14 is making:
// route every allocation through one function and you can replace the allocator
// underneath a working program.

// CLOX_HEAP_SIZE is the whole budget for the process. One megabyte is far more
// than the chapter needs and far less than a real program would; -D it to
// something small to watch exhaustion behave.
#ifndef CLOX_HEAP_SIZE
#define CLOX_HEAP_SIZE (1024 * 1024)
#endif

// heapAlloc, heapFree and heapRealloc mirror malloc/free/realloc, including the
// null and zero-size conventions: freeing NULL is a no-op, allocating zero
// bytes returns NULL, and heapRealloc(NULL, n) allocates.
//
// The arena is created lazily on the first allocation, so no caller has to
// remember to initialise it.
void* heapAlloc(size_t size);
void heapFree(void* pointer);
void* heapRealloc(void* pointer, size_t size);

// heapDestroy returns the arena to libc and resets everything, so the next
// allocation starts from a fresh heap. Only the tests use it; the interpreter
// lets the process exit do this.
void heapDestroy(void);

typedef struct {
  size_t arenaBytes;      // usable bytes between the sentinels
  size_t allocatedBytes;  // whole blocks, overhead included
  size_t freeBytes;
  size_t largestFree;     // the biggest single block: arenaBytes minus this is
                          // one way to see fragmentation
  int allocatedBlocks;
  int freeBlocks;
} HeapStats;

HeapStats heapStats(void);

// heapCheck walks both the implicit list of all blocks and the explicit list of
// free ones, and returns false if they disagree. It is the test's eyes: almost
// every way to get an allocator wrong corrupts an invariant long before it
// corrupts an answer.
bool heapCheck(void);

#endif
