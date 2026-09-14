#ifndef clox_debug_h
#define clox_debug_h

#include "chunk.h"

void disassembleChunk(Chunk* chunk, const char* name);

// disassembleInstruction returns the offset of the next instruction rather than
// taking a step size, because only it knows how wide the instruction it just
// read was. That return value is the whole reason a disassembler cannot simply
// loop over the code array: OP_RETURN is one byte, OP_CONSTANT is two, and
// OP_CONSTANT_LONG is four.
int disassembleInstruction(Chunk* chunk, int offset);

#endif
