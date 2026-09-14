#ifndef clox_line_h
#define clox_line_h

#include "common.h"

// LineArray is challenge 1: the source line for every byte of bytecode, stored
// as runs instead of one int per byte.
//
// The array the chapter writes is parallel to the code array -- byte i of code
// was compiled from line lines[i] -- which is simple and costs four bytes per
// bytecode byte. It is also almost entirely redundant, because bytecode is
// generated in source order: an expression statement compiles to a dozen bytes
// that all carry the same number, and a long function body can run to hundreds.
//
// So store each line once, with a count of how many consecutive bytes it
// covers. Lookup stops being an index and becomes a walk, which is the trade
// the challenge asks you to make knowingly: line numbers are only ever read to
// report an error, and a program that is about to print a stack trace and die
// can afford a loop.
typedef struct {
  int line;
  int count; // consecutive code bytes compiled from that line
} LineRun;

typedef struct {
  int capacity;
  int count; // number of runs, not the number of code bytes they cover
  LineRun* runs;
} LineArray;

void initLineArray(LineArray* array);

// writeLineArray records one more code byte as belonging to line.
void writeLineArray(LineArray* array, int line);

// lineArrayGet decodes the line for a byte offset, or -1 if the offset is past
// the last byte recorded. Callers in later chapters hold an instruction pointer
// that is always in range; -1 is here so that a bug shows up as an impossible
// line number instead of as a plausible one.
int lineArrayGet(const LineArray* array, int offset);

void freeLineArray(LineArray* array);

#endif
