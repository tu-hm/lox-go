#include <stdio.h>

#include "debug.h"
#include "value.h"

void disassembleChunk(FILE* out, Chunk* chunk, const char* name) {
  fprintf(out, "== %s ==\n", name);

  for (int offset = 0; offset < chunk->count;) {
    offset = disassembleInstruction(out, chunk, offset);
  }
}

static int constantInstruction(FILE* out, const char* name, Chunk* chunk,
                               int offset) {
  uint8_t constant = chunk->code[offset + 1];
  fprintf(out, "%-16s %4d '", name, constant);
  fprintValue(out, chunk->constants.values[constant]);
  fprintf(out, "'\n");
  return offset + 2;
}

static int constantLongInstruction(FILE* out, const char* name, Chunk* chunk,
                                   int offset) {
  int constant = chunk->code[offset + 1]
               | (chunk->code[offset + 2] << 8)
               | (chunk->code[offset + 3] << 16);
  fprintf(out, "%-16s %4d '", name, constant);
  fprintValue(out, chunk->constants.values[constant]);
  fprintf(out, "'\n");
  return offset + 4;
}

static int simpleInstruction(FILE* out, const char* name, int offset) {
  fprintf(out, "%s\n", name);
  return offset + 1;
}

int disassembleInstruction(FILE* out, Chunk* chunk, int offset) {
  fprintf(out, "%04d ", offset);

  // A bar instead of a repeated line number, so a run of instructions from one
  // source line reads as a block. This is also the only place the line encoding
  // is observable from outside, which is why the golden test is worth having:
  // challenge 1 changed how lines are stored and must not change this by a byte.
  int line = getLine(chunk, offset);
  if (offset > 0 && line == getLine(chunk, offset - 1)) {
    fprintf(out, "   | ");
  } else {
    fprintf(out, "%4d ", line);
  }

  uint8_t instruction = chunk->code[offset];
  switch (instruction) {
    case OP_CONSTANT:
      return constantInstruction(out, "OP_CONSTANT", chunk, offset);
    case OP_CONSTANT_LONG:
      return constantLongInstruction(out, "OP_CONSTANT_LONG", chunk, offset);
    case OP_ADD:
      return simpleInstruction(out, "OP_ADD", offset);
    case OP_SUBTRACT:
      return simpleInstruction(out, "OP_SUBTRACT", offset);
    case OP_MULTIPLY:
      return simpleInstruction(out, "OP_MULTIPLY", offset);
    case OP_DIVIDE:
      return simpleInstruction(out, "OP_DIVIDE", offset);
    case OP_NEGATE:
      return simpleInstruction(out, "OP_NEGATE", offset);
    case OP_RETURN:
      return simpleInstruction(out, "OP_RETURN", offset);
    default:
      // Not an assert. A disassembler is a debugging tool, and the moment it is
      // most useful is when the chunk is malformed -- so it reports the bad
      // byte, steps over it, and keeps going.
      fprintf(out, "Unknown opcode %d\n", instruction);
      return offset + 1;
  }
}
