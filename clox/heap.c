#include <stdlib.h>
#include <string.h>

#include "heap.h"

// Block layout, all sizes in bytes on a 64-bit host:
//
//     +--------+----------------------------+--------+
//     | header |          payload           | footer |
//     |   8    |        size - 16           |   8    |
//     +--------+----------------------------+--------+
//                  ^
//                  bp -- what callers get
//
// Header and footer hold the same word: the block's total size with the low bit
// set when the block is in use. Sizes are multiples of 16, so the low four bits
// are free to borrow.
//
// The footer is the whole reason coalescing is cheap. Merging with the *next*
// block is easy either way -- add the size in this header and you land on it.
// Merging with the *previous* one is the problem: there is no back pointer, and
// walking from the start of the arena to find it would make every free O(n).
// A trailing copy of the size solves it, because the word immediately before
// this block's header is that block's footer, and its size says where it began.
// Eight bytes per block buys O(1) coalescing in both directions.
//
// A free block additionally stores two pointers in its payload, which costs
// nothing: the payload of a free block is by definition not in use. That is
// what sets the minimum block size at 32 rather than 16.

typedef union {
  long double ld;
  void* p;
  size_t s;
} MaxAlign;

#define WSIZE (sizeof(size_t))       // header / footer
#define DSIZE (2 * WSIZE)            // alignment, and header + footer together
#define MIN_BLOCK (2 * DSIZE)        // header + prev + next + footer

#define PACK(size, alloc) ((size) | (alloc))
#define GET(p) (*(size_t*)(p))
#define PUT(p, val) (*(size_t*)(p) = (val))
#define GET_SIZE(p) (GET(p) & ~(size_t)0x7)
#define GET_ALLOC(p) (GET(p) & (size_t)0x1)

#define HDRP(bp) ((char*)(bp) - WSIZE)
#define FTRP(bp) ((char*)(bp) + GET_SIZE(HDRP(bp)) - DSIZE)
#define NEXT_BLKP(bp) ((char*)(bp) + GET_SIZE((char*)(bp) - WSIZE))
#define PREV_BLKP(bp) ((char*)(bp) - GET_SIZE((char*)(bp) - DSIZE))

// FreeNode lives in the payload of a free block, so the free list costs no
// memory of its own.
typedef struct FreeNode {
  struct FreeNode* prev;
  struct FreeNode* next;
} FreeNode;

static char* arena = NULL;     // what libc gave us, and what we give back
static char* blocks = NULL;    // first real block's payload pointer
static FreeNode* freeList = NULL;
static size_t arenaBytes = 0;

// ----------------------------------------------------------------- the list --

static void listInsert(void* bp) {
  FreeNode* node = (FreeNode*)bp;
  node->prev = NULL;
  node->next = freeList;
  if (freeList != NULL) freeList->prev = node;
  freeList = node;
}

static void listRemove(void* bp) {
  FreeNode* node = (FreeNode*)bp;
  if (node->prev != NULL) {
    node->prev->next = node->next;
  } else {
    freeList = node->next;
  }
  if (node->next != NULL) node->next->prev = node->prev;
}

// ------------------------------------------------------------- the arena --

static bool heapInit(void) {
  if (arena != NULL) return true;

  // Four extra words of room: one to align the payloads, two for the prologue
  // block, one for the epilogue header.
  size_t raw = CLOX_HEAP_SIZE + 4 * WSIZE + DSIZE;
  arena = (char*)malloc(raw);
  if (arena == NULL) return false;

  // Align so that payloads, not block starts, land on DSIZE boundaries: a
  // payload sits one word past its header, so the header must be odd-aligned.
  char* p = arena;
  while (((size_t)(p + WSIZE)) % DSIZE != 0) p++;

  size_t usable = raw - (size_t)(p - arena);
  usable = (usable - 4 * WSIZE) & ~(size_t)(DSIZE - 1);

  // The sentinels. A prologue block that is always allocated and an epilogue
  // header of size zero that is always allocated, so coalesce() can read the
  // block before and after any real block without ever asking whether one
  // exists. Every bounds check in free() is paid for once, here.
  PUT(p, PACK(DSIZE, 1));                     // prologue header
  PUT(p + WSIZE, PACK(DSIZE, 1));             // prologue footer
  blocks = p + DSIZE + WSIZE;                 // first real payload
  PUT(HDRP(blocks), PACK(usable, 0));
  PUT(FTRP(blocks), PACK(usable, 0));
  PUT((char*)blocks + usable - WSIZE, PACK(0, 1)); // epilogue header

  arenaBytes = usable;
  freeList = NULL;
  listInsert(blocks);
  return true;
}

void heapDestroy(void) {
  free(arena);
  arena = NULL;
  blocks = NULL;
  freeList = NULL;
  arenaBytes = 0;
}

// -------------------------------------------------------------- coalescing --

// coalesce merges bp with whichever neighbours are free, inserts the result
// into the free list, and returns it. bp must already be marked free and must
// not be in the list.
static void* coalesce(void* bp) {
  size_t prevAlloc = GET_ALLOC(FTRP(PREV_BLKP(bp)));
  size_t nextAlloc = GET_ALLOC(HDRP(NEXT_BLKP(bp)));
  size_t size = GET_SIZE(HDRP(bp));

  if (prevAlloc && nextAlloc) {
    // nothing to merge
  } else if (prevAlloc && !nextAlloc) {
    listRemove(NEXT_BLKP(bp));
    size += GET_SIZE(HDRP(NEXT_BLKP(bp)));
    PUT(HDRP(bp), PACK(size, 0));
    PUT(FTRP(bp), PACK(size, 0));
  } else if (!prevAlloc && nextAlloc) {
    listRemove(PREV_BLKP(bp));
    size += GET_SIZE(HDRP(PREV_BLKP(bp)));
    bp = PREV_BLKP(bp);
    PUT(HDRP(bp), PACK(size, 0));
    PUT(FTRP(bp), PACK(size, 0));
  } else {
    listRemove(PREV_BLKP(bp));
    listRemove(NEXT_BLKP(bp));
    size += GET_SIZE(HDRP(PREV_BLKP(bp))) + GET_SIZE(HDRP(NEXT_BLKP(bp)));
    bp = PREV_BLKP(bp);
    PUT(HDRP(bp), PACK(size, 0));
    PUT(FTRP(bp), PACK(size, 0));
  }

  listInsert(bp);
  return bp;
}

// ------------------------------------------------------------ allocation --

// roundBlock returns the block size needed to hold size payload bytes, or 0 if
// that cannot be represented. Rounding up is two additions, and either can wrap
// on an absurd request -- which reaches here as a block size far smaller than
// asked for, and an allocation that silently fits. Callers read 0 as failure.
static size_t roundBlock(size_t size) {
  if (size > SIZE_MAX - DSIZE - (DSIZE - 1)) return 0;

  size_t needed = size + DSIZE; // header and footer
  if (needed < MIN_BLOCK) return MIN_BLOCK;
  return (needed + (DSIZE - 1)) & ~(size_t)(DSIZE - 1);
}

// findFit is first fit, which is the honest starting point: it is the simplest
// policy that works, and every better one -- best fit, segregated lists by size
// class, address-ordered insertion -- is a response to a fragmentation pattern
// you have to be able to measure first. heapStats() is what would measure it.
static void* findFit(size_t asize) {
  for (FreeNode* node = freeList; node != NULL; node = node->next) {
    if (GET_SIZE(HDRP(node)) >= asize) return node;
  }
  return NULL;
}

// place marks bp allocated at asize, splitting off the remainder when what is
// left could hold a block of its own. Without the split, asking for 32 bytes
// out of a one-megabyte free block would cost the whole megabyte.
static void place(void* bp, size_t asize) {
  size_t csize = GET_SIZE(HDRP(bp));
  listRemove(bp);

  if (csize - asize >= MIN_BLOCK) {
    PUT(HDRP(bp), PACK(asize, 1));
    PUT(FTRP(bp), PACK(asize, 1));

    void* rest = NEXT_BLKP(bp);
    PUT(HDRP(rest), PACK(csize - asize, 0));
    PUT(FTRP(rest), PACK(csize - asize, 0));
    listInsert(rest);
  } else {
    // The remainder is too small to be a block, so it goes to the caller as
    // slack rather than becoming unusable memory nothing can reach.
    PUT(HDRP(bp), PACK(csize, 1));
    PUT(FTRP(bp), PACK(csize, 1));
  }
}

void* heapAlloc(size_t size) {
  if (size == 0) return NULL;
  if (!heapInit()) return NULL;

  size_t asize = roundBlock(size);
  if (asize == 0) return NULL;

  void* bp = findFit(asize);
  if (bp == NULL) return NULL;

  place(bp, asize);
  return bp;
}

void heapFree(void* pointer) {
  if (pointer == NULL) return;

  size_t size = GET_SIZE(HDRP(pointer));
  PUT(HDRP(pointer), PACK(size, 0));
  PUT(FTRP(pointer), PACK(size, 0));
  coalesce(pointer);
}

void* heapRealloc(void* pointer, size_t size) {
  if (pointer == NULL) return heapAlloc(size);
  if (size == 0) {
    heapFree(pointer);
    return NULL;
  }

  size_t asize = roundBlock(size);
  if (asize == 0) return NULL;

  size_t csize = GET_SIZE(HDRP(pointer));

  // Shrinking in place, splitting off the tail if it is worth a block.
  if (asize <= csize) {
    if (csize - asize >= MIN_BLOCK) {
      PUT(HDRP(pointer), PACK(asize, 1));
      PUT(FTRP(pointer), PACK(asize, 1));

      void* rest = NEXT_BLKP(pointer);
      PUT(HDRP(rest), PACK(csize - asize, 1)); // marked allocated so that
      PUT(FTRP(rest), PACK(csize - asize, 1)); // coalesce() sees a real block
      heapFree(rest);
    }
    return pointer;
  }

  // Growing in place when the next block is free and big enough. This is the
  // case that matters here: GROW_ARRAY doubles a chunk repeatedly, and the
  // block being doubled is very often the most recently allocated one, with
  // nothing but free space after it. Without this, every growth copies.
  void* next = NEXT_BLKP(pointer);
  if (!GET_ALLOC(HDRP(next)) && csize + GET_SIZE(HDRP(next)) >= asize) {
    size_t combined = csize + GET_SIZE(HDRP(next));
    listRemove(next);
    PUT(HDRP(pointer), PACK(combined, 1));
    PUT(FTRP(pointer), PACK(combined, 1));

    if (combined - asize >= MIN_BLOCK) {
      PUT(HDRP(pointer), PACK(asize, 1));
      PUT(FTRP(pointer), PACK(asize, 1));
      void* rest = NEXT_BLKP(pointer);
      PUT(HDRP(rest), PACK(combined - asize, 0));
      PUT(FTRP(rest), PACK(combined - asize, 0));
      listInsert(rest);
    }
    return pointer;
  }

  void* fresh = heapAlloc(size);
  if (fresh == NULL) return NULL;
  memcpy(fresh, pointer, csize - DSIZE);
  heapFree(pointer);
  return fresh;
}

// ------------------------------------------------------------ inspection --

HeapStats heapStats(void) {
  HeapStats stats;
  memset(&stats, 0, sizeof(stats));
  if (arena == NULL) return stats;

  stats.arenaBytes = arenaBytes;
  for (char* bp = blocks; GET_SIZE(HDRP(bp)) > 0; bp = NEXT_BLKP(bp)) {
    size_t size = GET_SIZE(HDRP(bp));
    if (GET_ALLOC(HDRP(bp))) {
      stats.allocatedBytes += size;
      stats.allocatedBlocks++;
    } else {
      stats.freeBytes += size;
      stats.freeBlocks++;
      if (size > stats.largestFree) stats.largestFree = size;
    }
  }
  return stats;
}

bool heapCheck(void) {
  if (arena == NULL) return true;

  int freeSeen = 0;
  bool prevFree = false;
  size_t total = 0;

  for (char* bp = blocks; GET_SIZE(HDRP(bp)) > 0; bp = NEXT_BLKP(bp)) {
    size_t size = GET_SIZE(HDRP(bp));

    if (size < MIN_BLOCK) return false;
    if (size % DSIZE != 0) return false;
    if ((size_t)bp % DSIZE != 0) return false;
    // Header and footer must agree, or the previous block cannot be found.
    if (GET(HDRP(bp)) != GET(FTRP(bp))) return false;

    bool isFree = !GET_ALLOC(HDRP(bp));
    // Two free blocks side by side mean a free() failed to coalesce, which is
    // invisible until the day an allocation that should have fit does not.
    if (isFree && prevFree) return false;
    if (isFree) freeSeen++;
    prevFree = isFree;
    total += size;
  }

  if (total != arenaBytes) return false;

  int listed = 0;
  for (FreeNode* node = freeList; node != NULL; node = node->next) {
    if (GET_ALLOC(HDRP(node))) return false; // an allocated block on the list
    if (node->next != NULL && node->next->prev != node) return false;
    listed++;
    if (listed > freeSeen) return false; // a cycle
  }
  return listed == freeSeen;
}
