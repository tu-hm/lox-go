#include "chunk.h"
#include "memory.h"

void initChunk(Chunk* chunk) {
  chunk->count = 0;
  chunk->capacity = 0;
  chunk->code = NULL;
  initLineArray(&chunk->lines);
  initValueArray(&chunk->constants);
}

void freeChunk(Chunk* chunk) {
  FREE_ARRAY(uint8_t, chunk->code, chunk->capacity);
  freeLineArray(&chunk->lines);
  freeValueArray(&chunk->constants);
  // Zeroing rather than leaving the freed pointers in place: a chunk is left in
  // the same state initChunk produces, so freeing one twice is harmless and
  // reusing one is legal.
  initChunk(chunk);
}

void writeChunk(Chunk* chunk, uint8_t byte, int line) {
  if (chunk->capacity < chunk->count + 1) {
    int oldCapacity = chunk->capacity;
    chunk->capacity = GROW_CAPACITY(oldCapacity);
    chunk->code = GROW_ARRAY(uint8_t, chunk->code, oldCapacity,
                             chunk->capacity);
  }

  chunk->code[chunk->count] = byte;
  chunk->count++;
  writeLineArray(&chunk->lines, line);
}

int addConstant(Chunk* chunk, Value value) {
  writeValueArray(&chunk->constants, value);
  return chunk->constants.count - 1;
}

bool writeConstant(Chunk* chunk, Value value, int line) {
  // The bound is checked before the value is interned, not after. Adding it
  // first and then refusing to emit would leave a constant in the pool that no
  // instruction refers to, and the compiler's answer to a full pool is to
  // report and keep parsing -- so that garbage would accumulate.
  if (chunk->constants.count > 0xffffff) return false;

  int constant = addConstant(chunk, value);

  if (constant < 256) {
    writeChunk(chunk, OP_CONSTANT, line);
    writeChunk(chunk, (uint8_t)constant, line);
    return true;
  }

  // Three bytes, least significant first. Little-endian is not the obvious
  // choice -- big-endian reads more naturally in a hex dump -- but it matches
  // the host on every machine clox is likely to run on, so chapter 15's
  // dispatch loop can eventually read the operand with one unaligned load
  // rather than three shifts.
  writeChunk(chunk, OP_CONSTANT_LONG, line);
  writeChunk(chunk, (uint8_t)(constant & 0xff), line);
  writeChunk(chunk, (uint8_t)((constant >> 8) & 0xff), line);
  writeChunk(chunk, (uint8_t)((constant >> 16) & 0xff), line);
  return true;
}

int getLine(const Chunk* chunk, int offset) {
  return lineArrayGet(&chunk->lines, offset);
}
