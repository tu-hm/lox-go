#include <stdio.h>
#include <stdlib.h>

#include "memory.h"

#ifdef CLOX_OWN_HEAP
#include "heap.h"
#endif

void* reallocate(void* pointer, size_t oldSize, size_t newSize) {
  if (newSize == 0) {
#ifdef CLOX_OWN_HEAP
    heapFree(pointer);
#else
    free(pointer);
#endif
    return NULL;
  }

#ifdef CLOX_OWN_HEAP
  void* result = heapRealloc(pointer, newSize);
#else
  void* result = realloc(pointer, newSize);
#endif

  // Out of memory is not a Lox error -- there is no interpreter state left to
  // report it with, and no way for a Lox program to have caused or handled it.
  // The book exits silently; a word on stderr costs nothing and turns an
  // unexplained exit code into a diagnosis.
  if (result == NULL) {
    fprintf(stderr, "clox: out of memory (requested %zu bytes)\n", newSize);
    exit(1);
  }
  return result;
}
