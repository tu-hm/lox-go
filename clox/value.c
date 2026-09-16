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

// %g prints 1.2 as "1.2" and 1.0 as "1". That differs from the Go tree-walker's
// strconv.FormatFloat(v, 'f', -1, 64) only for very large and very small
// magnitudes, where %g switches to exponent form -- and %g also rounds to six
// significant digits, so -0.8214285714285716 prints as -0.821429.
//
// As of chapter 15 this is user-visible output rather than only disassembly:
// OP_RETURN prints what it pops. The two implementations still cannot be run on
// the same program, so nothing compares them yet. Chapter 17 is the first time
// they could, and the rounding is what will differ first.
void fprintValue(FILE* out, Value value) {
  fprintf(out, "%g", value);
}

void printValue(Value value) {
  fprintValue(stdout, value);
}
