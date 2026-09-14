// Unit tests for the challenge 4 allocator.
//
// The golden disassembly test cannot see any of this: the interpreter produces
// identical output whether reallocate() calls libc or heap.c, which is the
// point of that test and the reason this one has to exist separately. Almost
// every way to get an allocator wrong shows up first as a broken invariant and
// only much later as a wrong answer, so most assertions here are heapCheck().

#include <stdio.h>
#include <string.h>

#include "heap.h"

static int failures = 0;
static const char* current = "";

#define CHECK(cond)                                                      \
  do {                                                                   \
    if (!(cond)) {                                                       \
      printf("  FAIL %s: %s (line %d)\n", current, #cond, __LINE__);     \
      failures++;                                                        \
    }                                                                    \
  } while (0)

#define RUN(fn)        \
  do {                 \
    current = #fn;     \
    heapDestroy();     \
    fn();              \
    heapDestroy();     \
  } while (0)

static void allocationsAreMaxAligned(void) {
  // Every size class, not just the convenient ones: a caller asking for 1 byte
  // must still get a pointer a double could be stored at.
  for (size_t size = 1; size <= 256; size++) {
    void* p = heapAlloc(size);
    CHECK(p != NULL);
    CHECK((size_t)p % 16 == 0);
    heapFree(p);
  }
  CHECK(heapCheck());
}

static void allocatingZeroBytesReturnsNull(void) {
  CHECK(heapAlloc(0) == NULL);
  heapFree(NULL); // must not crash
  CHECK(heapCheck());
}

static void allocationSplitsRatherThanTakingTheWholeBlock(void) {
  HeapStats before = heapStats();
  void* p = heapAlloc(32);
  HeapStats after = heapStats();

  CHECK(p != NULL);
  CHECK(before.arenaBytes == 0); // nothing allocated yet, so no arena yet
  CHECK(after.allocatedBlocks == 1);
  CHECK(after.allocatedBytes <= 64); // 32 payload + header + footer, rounded
  CHECK(after.freeBlocks == 1);      // the remainder, not swallowed
  CHECK(heapCheck());
  heapFree(p);
}

static void freeingCoalescesWithTheNextBlock(void) {
  void* a = heapAlloc(64);
  void* b = heapAlloc(64);
  void* c = heapAlloc(64);
  CHECK(heapStats().freeBlocks == 1); // only the tail

  heapFree(b);
  CHECK(heapStats().freeBlocks == 2);

  heapFree(a); // a is adjacent to the free b, so they must merge
  CHECK(heapStats().freeBlocks == 2);
  CHECK(heapCheck());

  heapFree(c);
}

static void freeingCoalescesWithThePreviousBlock(void) {
  void* a = heapAlloc(64);
  void* b = heapAlloc(64);
  void* c = heapAlloc(64);

  heapFree(a);
  CHECK(heapStats().freeBlocks == 2);

  // This is the direction that needs the footer: b has no pointer back to a.
  heapFree(b);
  CHECK(heapStats().freeBlocks == 2);
  CHECK(heapCheck());

  heapFree(c);
}

static void freeingCoalescesWithBothNeighbours(void) {
  void* a = heapAlloc(64);
  void* b = heapAlloc(64);
  void* c = heapAlloc(64);

  heapFree(a);
  heapFree(c); // merges with the tail, so this is 2 free blocks and not 3
  CHECK(heapStats().freeBlocks == 2);

  heapFree(b); // must absorb a on the left and c on the right at once
  HeapStats stats = heapStats();
  CHECK(stats.freeBlocks == 1);
  CHECK(stats.allocatedBlocks == 0);
  CHECK(heapCheck());
}

static void freeingEverythingRestoresOneWholeBlock(void) {
  // The strongest statement the allocator can make: after n allocations and n
  // frees in any order, the heap is indistinguishable from a fresh one.
  void* ptrs[64];
  for (int i = 0; i < 64; i++) {
    ptrs[i] = heapAlloc((size_t)(i * 7 + 1));
    CHECK(ptrs[i] != NULL);
  }
  for (int i = 63; i >= 0; i -= 2) heapFree(ptrs[i]);
  for (int i = 0; i < 64; i += 2) heapFree(ptrs[i]);

  HeapStats stats = heapStats();
  CHECK(stats.allocatedBlocks == 0);
  CHECK(stats.freeBlocks == 1);
  CHECK(stats.largestFree == stats.arenaBytes);
  CHECK(heapCheck());
}

static void freedBlocksAreReused(void) {
  void* a = heapAlloc(128);
  heapFree(a);
  void* b = heapAlloc(128);
  CHECK(a == b); // first fit on a coalesced heap must land back on the same spot
  heapFree(b);
  CHECK(heapCheck());
}

static void reallocGrowsInPlaceWhenTheNextBlockIsFree(void) {
  void* p = heapAlloc(64);
  memset(p, 0xab, 64);

  void* grown = heapRealloc(p, 512);
  CHECK(grown == p); // nothing follows it but free space, so no copy
  CHECK(((unsigned char*)grown)[0] == 0xab);
  CHECK(((unsigned char*)grown)[63] == 0xab);
  CHECK(heapCheck());
  heapFree(grown);
}

static void reallocCopiesWhenItCannotGrowInPlace(void) {
  void* a = heapAlloc(64);
  void* blocker = heapAlloc(64); // pins a's right-hand neighbour
  memset(a, 0xcd, 64);

  void* grown = heapRealloc(a, 4096);
  CHECK(grown != a);
  CHECK(((unsigned char*)grown)[0] == 0xcd);
  CHECK(((unsigned char*)grown)[63] == 0xcd); // every old byte survived
  CHECK(heapCheck());

  heapFree(grown);
  heapFree(blocker);
}

static void reallocShrinkingReleasesTheTail(void) {
  void* p = heapAlloc(1024);
  HeapStats before = heapStats();

  void* small = heapRealloc(p, 32);
  HeapStats after = heapStats();

  CHECK(small == p); // shrinking never moves
  CHECK(after.allocatedBytes < before.allocatedBytes);
  CHECK(after.freeBytes > before.freeBytes);
  CHECK(heapCheck());
  heapFree(small);
}

static void reallocFollowsTheNullAndZeroConventions(void) {
  void* p = heapRealloc(NULL, 64);
  CHECK(p != NULL); // realloc(NULL, n) allocates

  CHECK(heapRealloc(p, 0) == NULL); // realloc(p, 0) frees
  HeapStats stats = heapStats();
  CHECK(stats.allocatedBlocks == 0);
  CHECK(heapCheck());
}

static void exhaustionReturnsNullRatherThanCorrupting(void) {
  CHECK(heapAlloc(CLOX_HEAP_SIZE * 2) == NULL);

  // And the heap is still usable afterwards -- a failed allocation must not
  // leave the free list half-updated.
  void* p = heapAlloc(64);
  CHECK(p != NULL);
  CHECK(heapCheck());
  heapFree(p);
}

static void anImpossibleSizeFailsInsteadOfWrapping(void) {
  // Rounding a request up to a block size is two additions, and both wrap near
  // SIZE_MAX. A wrapped size asks for a tiny block, finds one, and hands the
  // caller a pointer to far less memory than it asked for -- a buffer overrun
  // manufactured by the allocator itself.
  CHECK(heapAlloc(SIZE_MAX) == NULL);
  CHECK(heapAlloc(SIZE_MAX - 8) == NULL);

  void* p = heapAlloc(64);
  CHECK(p != NULL);
  CHECK(heapRealloc(p, SIZE_MAX) == NULL);
  CHECK(heapCheck());
  heapFree(p);
}

static void churnKeepsEveryInvariant(void) {
  // A deterministic pseudo-random workload, which is the closest thing here to
  // how the interpreter actually behaves: many small blocks, repeatedly grown.
  void* live[32];
  memset(live, 0, sizeof(live));
  unsigned seed = 12345;

  for (int i = 0; i < 4000; i++) {
    seed = seed * 1103515245u + 12345u;
    int slot = (int)((seed >> 16) % 32);
    size_t size = (size_t)((seed >> 8) % 512) + 1;

    if (live[slot] == NULL) {
      live[slot] = heapAlloc(size);
    } else if (size % 3 == 0) {
      void* grown = heapRealloc(live[slot], size);
      if (grown != NULL) live[slot] = grown;
    } else {
      heapFree(live[slot]);
      live[slot] = NULL;
    }

    if (i % 200 == 0) CHECK(heapCheck());
  }

  for (int i = 0; i < 32; i++) heapFree(live[i]);
  HeapStats stats = heapStats();
  CHECK(stats.allocatedBlocks == 0);
  CHECK(stats.freeBlocks == 1); // no fragmentation survives a full free
  CHECK(heapCheck());
}

int main(int argc, const char* argv[]) {
  // Unbuffered, because the interesting failures here are segfaults: a buffered
  // stdout is discarded when the process dies, so the test that was running and
  // everything it had already reported would be lost exactly when it is needed.
  setvbuf(stdout, NULL, _IONBF, 0);

  RUN(allocationsAreMaxAligned);
  RUN(allocatingZeroBytesReturnsNull);
  RUN(allocationSplitsRatherThanTakingTheWholeBlock);
  RUN(freeingCoalescesWithTheNextBlock);
  RUN(freeingCoalescesWithThePreviousBlock);
  RUN(freeingCoalescesWithBothNeighbours);
  RUN(freeingEverythingRestoresOneWholeBlock);
  RUN(freedBlocksAreReused);
  RUN(reallocGrowsInPlaceWhenTheNextBlockIsFree);
  RUN(reallocCopiesWhenItCannotGrowInPlace);
  RUN(reallocShrinkingReleasesTheTail);
  RUN(reallocFollowsTheNullAndZeroConventions);
  RUN(exhaustionReturnsNullRatherThanCorrupting);
  RUN(anImpossibleSizeFailsInsteadOfWrapping);
  RUN(churnKeepsEveryInvariant);

  if (failures > 0) {
    printf("heap: %d failure(s)\n", failures);
    return 1;
  }
  printf("heap: ok\n");
  return 0;
}
