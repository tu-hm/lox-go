// test_compile is the golden driver for chapter 17: a list of expressions, each
// compiled, disassembled and run.
//
// It exists for the same reason test_chunk.c does. The compiler handles exactly
// one expression per source text and then requires end of input, so covering
// precedence, associativity, unary chains, the 24-bit constant operand and the
// error paths through the CLI would mean twenty .lox files and twenty golden
// files. One binary and two golden files -- stdout and stderr -- cover all of it,
// and both must be byte for byte identical in every build configuration.
//
// It is a test binary in the same sense test_heap.c is: it links the same
// objects the interpreter does, minus main.o.

#include <stdio.h>

#include "chunk.h"
#include "common.h"
#include "compiler.h"
#include "debug.h"
#include "vm.h"

#ifdef CLOX_OWN_HEAP
#include "heap.h"
#endif

// expr compiles one expression, prints its bytecode, and runs it. The name is
// separate from the source so a source with a newline in it still produces one
// header line.
static void expr(const char* name, const char* source) {
  Chunk chunk;
  initChunk(&chunk);

  if (compile(source, &chunk)) {
    disassembleChunk(stdout, &chunk, name);
    interpretChunk(&chunk);
  } else {
    // A chunk that failed to compile has holes in it; disassembling one prints
    // whichever bytes happened to land beside each other.
    printf("== %s ==\ncompile error\n", name);
  }

  printf("\n");
  freeChunk(&chunk);
}

// badExpr is expr for the sources that must not compile. The header goes to
// stderr as well, so the message golden says which source produced which
// message, and so a message that moves is a diff rather than a mystery.
static void badExpr(const char* name, const char* source) {
  fprintf(stderr, "-- %s\n", name);
  expr(name, source);
}

// disassembleWindow prints instructions first..last of a chunk and steps
// silently over the rest.
//
// The stepping still goes through disassembleInstruction, because that is the
// only function that knows how wide an instruction is -- it just writes to
// /dev/null. Scanning the code array for a particular opcode byte instead would
// be exactly the mistake chapter 14 exists to rule out: an operand byte can hold
// any value an opcode can.
static void disassembleWindow(Chunk* chunk, int first, int last) {
  FILE* sink = fopen("/dev/null", "w");
  if (sink == NULL) sink = stdout;

  int offset = 0;
  for (int index = 0; offset < chunk->count; index++) {
    FILE* out = (index >= first && index <= last) ? stdout : sink;
    offset = disassembleInstruction(out, chunk, offset);
  }

  if (sink != stdout) fclose(sink);
}

#define TERMS 300

// manyConstants puts chapter 14's challenge 2 through the compiler for the first
// time. `0 + 1 + 2 + ... + 299` interns 300 constants -- addConstant does not
// deduplicate, so the count is the number of literals, not the number of
// distinct values -- and the operand has to widen at index 256.
//
// Two things are being checked. The window shows the compiler choosing the
// instruction, and the printed sum shows the VM decoding the operand it chose:
// a three-byte operand read in the wrong order would still run, and would load
// some other constant.
static void manyConstants(void) {
  static char source[8 * TERMS];
  int length = 0;
  for (int i = 0; i < TERMS; i++) {
    length += snprintf(source + length, sizeof(source) - (size_t)length,
                       i == 0 ? "%d" : " + %d", i);
  }

  Chunk chunk;
  initChunk(&chunk);

  if (!compile(source, &chunk)) {
    printf("== %d constants ==\ncompile error\n\n", TERMS);
    freeChunk(&chunk);
    return;
  }

  printf("== %d constants ==\n", TERMS);

  // The instructions alternate: the first constant, then (constant, add) for
  // each term after it. So the nth constant load is instruction 2n-1, and the
  // first one that needs a three-byte operand is n = 256.
  int firstLong = 2 * 256 - 1;
  disassembleWindow(&chunk, firstLong - 3, firstLong + 1);

  printf("-- %d code bytes, %d constants\n", chunk.count,
         chunk.constants.count);
  interpretChunk(&chunk);
  printf("\n");

  freeChunk(&chunk);
}

int main(void) {
  initVM();

  // Precedence and grouping.
  expr("1", "1");
  expr("1 + 2", "1 + 2");
  expr("1 + 2 * 3", "1 + 2 * 3");
  expr("(1 + 2) * 3", "(1 + 2) * 3");

  // Associativity. Parse the right operand at the operator's own precedence
  // instead of one tighter and these become 2 and 8 rather than -4 and 2.
  expr("1 - 2 - 3", "1 - 2 - 3");
  expr("12 / 3 / 2", "12 / 3 / 2");

  // Unary binds tighter than any binary operator, and stacks.
  expr("-1 + 2", "-1 + 2");
  expr("-(1 + 2)", "-(1 + 2)");
  expr("- - 3", "- - 3");

  // Challenge 1's expression, and the book's own corpus file
  // test/expressions/evaluate.lox, which expects 2.
  expr("(-1 + 2) * 3 - -4", "(-1 + 2) * 3 - -4");
  expr("(5 - (3 - 1)) + -1", "(5 - (3 - 1)) + -1");

  expr("1.2 + 3.4", "1.2 + 3.4");

  // Line attribution, and the case that actually pins it down. OP_ADD is emitted
  // after the whole right operand has been consumed, so parser.previous is `2`
  // on line 1 while parser.current is already `-` on line 2. Blaming the wrong
  // one of those two puts the instruction on the wrong line, and `1\n+ 2` would
  // not notice, because there the two tokens are on the same line.
  expr("1 + 2 <newline> - 3", "1 + 2\n- 3");

  manyConstants();

  // One message each, which is what panicMode is for. `1 + + 2` is the clearest
  // of them: the parser reports the missing operand and keeps going, so without
  // panicMode the failed consume of EOF would report a second time.
  badExpr("<empty>", "");
  badExpr("1 +", "1 +");
  badExpr("(1", "(1");
  badExpr("1 2", "1 2");
  badExpr(")", ")");
  badExpr("1 + + 2", "1 + + 2");

  // A scanner error is a token, and advance() is what turns it into a parse
  // error. The rest of the line still scans; panicMode is what keeps it quiet.
  badExpr("@", "@");
  badExpr("\"unterminated", "\"unterminated");

  // Chapter 18 and 19 make these expressions. Today the table has no prefix rule
  // for either, which is the one error a NULL entry reports by itself.
  badExpr("true", "true");
  badExpr("\"abc\"", "\"abc\"");

  // Challenge 3. The parse is correct and the codegen is missing, so the error
  // comes from ternary() and not from a confused parser -- `2 * 3` is consumed
  // as one operand, and there is no second message.
  badExpr("1 ? 2 * 3 : 4", "1 ? 2 * 3 : 4");
  badExpr("1 ? 2", "1 ? 2");

  freeVM();

#ifdef CLOX_OWN_HEAP
  HeapStats stats = heapStats();
  if (stats.allocatedBlocks != 0 || !heapCheck()) {
    fprintf(stderr, "test_compile: %d block(s) still live at exit\n",
            stats.allocatedBlocks);
    return 1;
  }
#endif
  return 0;
}
