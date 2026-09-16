#ifndef clox_value_h
#define clox_value_h

#include <stdio.h>

#include "common.h"

// Value is a bare double for now, and that is the whole point of a constant
// pool being an array rather than a list of pointers: 300 constants are 2400
// contiguous bytes, so walking them touches a handful of cache lines instead of
// chasing 300 of them.
//
// Chapter 18 replaces this with a tagged union to make room for booleans and
// nil, and chapter 19 adds a pointer case for heap-allocated objects. Both are
// deliberately still ahead: every use site below is written against the
// typedef, so the size of that change is a measure of how well this one is
// contained.
typedef double Value;

typedef struct {
  int capacity;
  int count;
  Value* values;
} ValueArray;

void initValueArray(ValueArray* array);
void writeValueArray(ValueArray* array, Value value);
void freeValueArray(ValueArray* array);
// fprintValue is the primitive; printValue is it aimed at stdout. The split
// exists because the disassembler now serves two masters -- see debug.h.
void fprintValue(FILE* out, Value value);
void printValue(Value value);

#endif
