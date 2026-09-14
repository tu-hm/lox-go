#ifndef clox_memory_h
#define clox_memory_h

#include "common.h"

// GROW_CAPACITY is the growth strategy for every dynamic array here: eight
// elements to start, then double. Doubling is what makes a sequence of appends
// amortise to O(1) per element -- growing by a constant would make writing n
// bytes cost O(n^2), since each regrow copies everything written so far.
//
// Eight is the floor because the copy is not the only cost. A chunk that only
// ever holds two bytes still pays for a call into the allocator, and starting
// at one would pay for four of them.
#define GROW_CAPACITY(capacity) \
    ((capacity) < 8 ? 8 : (capacity) * 2)

#define GROW_ARRAY(type, pointer, oldCount, newCount) \
    (type*)reallocate(pointer, sizeof(type) * (oldCount), \
        sizeof(type) * (newCount))

#define FREE_ARRAY(type, pointer, oldCount) \
    reallocate(pointer, sizeof(type) * (oldCount), 0)

// reallocate is the only function in clox that allocates. Every array growth,
// every free, and everything later chapters add goes through here, which is why
// the macros above are so thin: they exist to compute sizes, not to allocate.
//
// Routing all four operations -- allocate, free, grow, shrink -- through one
// signature is what makes the garbage collector of chapter 26 possible, and it
// is what lets heap.c replace the allocator wholesale without any other file
// knowing. The oldSize argument is unused by both implementations today; the
// collector is what will read it.
void* reallocate(void* pointer, size_t oldSize, size_t newSize);

#endif
