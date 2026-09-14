#ifndef clox_chunk_h
#define clox_chunk_h

#include "common.h"
#include "line.h"
#include "value.h"

typedef enum {
  OP_CONSTANT,
  OP_CONSTANT_LONG,
  OP_RETURN,
} OpCode;

// Chunk is a sequence of bytecode plus everything that sequence refers to. The
// code array holds opcodes and their operands with no separation between them,
// which is what "byte" code means: an instruction is one byte, and whatever
// follows it is decided by that byte alone.
typedef struct {
  int count;
  int capacity;
  uint8_t* code;
  LineArray lines;
  ValueArray constants;
} Chunk;

void initChunk(Chunk* chunk);
void freeChunk(Chunk* chunk);
void writeChunk(Chunk* chunk, uint8_t byte, int line);

// addConstant interns a value in the pool and returns its index. It emits no
// code, so a caller that wants an instruction wants writeConstant instead.
int addConstant(Chunk* chunk, Value value);

// writeConstant is challenge 2: add a constant and emit the instruction that
// loads it, choosing the operand width. Prefer it over addConstant plus a
// hand-written OP_CONSTANT, which silently truncates past index 255.
void writeConstant(Chunk* chunk, Value value, int line);

// getLine is the line a byte of code was compiled from. See LineArray.
int getLine(const Chunk* chunk, int offset);

#endif
