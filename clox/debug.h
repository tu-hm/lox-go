#ifndef clox_debug_h
#define clox_debug_h

#include <stdio.h>

#include "chunk.h"

// Both of these take the stream to write to rather than assuming stdout. The
// disassembler has two callers with opposite needs: a standalone dump is the
// program's output, and the VM's execution trace is diagnostic noise that must
// stay out of it. Writing the trace to stderr is what keeps every build
// configuration's stdout byte for byte identical, which is the claim
// `make test HEAP=own` and `make test MODE=release` exist to check.
void disassembleChunk(FILE* out, Chunk* chunk, const char* name);

// disassembleInstruction returns the offset of the next instruction rather than
// taking a step size, because only it knows how wide the instruction it just
// read was. That return value is the whole reason a disassembler cannot simply
// loop over the code array: OP_RETURN is one byte, OP_CONSTANT is two, and
// OP_CONSTANT_LONG is four.
int disassembleInstruction(FILE* out, Chunk* chunk, int offset);

#endif
