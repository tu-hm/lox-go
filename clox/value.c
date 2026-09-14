#include <stdio.h>

#include "memory.h"
#include "value.h"

void initValueArray(ValueArray* array) {
  array->values = NULL;
  array->capacity = 0;
  array->count = 0;
}

void writeValueArray(ValueArray* array, Value value) {
  if (array->capacity < array->count + 1) {
    int oldCapacity = array->capacity;
    array->capacity = GROW_CAPACITY(oldCapacity);
    array->values = GROW_ARRAY(Value, array->values, oldCapacity,
                               array->capacity);
  }

  array->values[array->count] = value;
  array->count++;
}

void freeValueArray(ValueArray* array) {
  FREE_ARRAY(Value, array->values, array->capacity);
  initValueArray(array);
}

// printValue uses %g, which prints 1.2 as "1.2" and 1.0 as "1". That differs
// from the Go tree-walker's strconv.FormatFloat(v, 'f', -1, 64) only for very
// large and very small magnitudes, where %g switches to exponent form. Nothing
// compares the two today; chapter 15 is where it would start to matter.
void printValue(Value value) {
  printf("%g", value);
}
