#include "line.h"
#include "memory.h"

void initLineArray(LineArray* array) {
  array->runs = NULL;
  array->capacity = 0;
  array->count = 0;
}

void writeLineArray(LineArray* array, int line) {
  // The common case by far: the byte just written came from the same line as
  // the one before it, so there is no new run, only a longer one.
  if (array->count > 0 && array->runs[array->count - 1].line == line) {
    array->runs[array->count - 1].count++;
    return;
  }

  if (array->capacity < array->count + 1) {
    int oldCapacity = array->capacity;
    array->capacity = GROW_CAPACITY(oldCapacity);
    array->runs = GROW_ARRAY(LineRun, array->runs, oldCapacity,
                             array->capacity);
  }

  array->runs[array->count].line = line;
  array->runs[array->count].count = 1;
  array->count++;
}

int lineArrayGet(const LineArray* array, int offset) {
  // A linear walk, not a binary search. The runs are sorted by the offset range
  // they cover, so a search would work and would be O(log n) -- but n is the
  // number of *lines* in a function, this runs once per error, and the constant
  // factor on a walk over a packed array is very small. Chapter 15's error
  // reporting is the first caller; if it ever shows up in a profile, the array
  // is already in the right shape to search.
  for (int i = 0; i < array->count; i++) {
    offset -= array->runs[i].count;
    if (offset < 0) return array->runs[i].line;
  }
  return -1;
}

void freeLineArray(LineArray* array) {
  FREE_ARRAY(LineRun, array->runs, array->capacity);
  initLineArray(array);
}
