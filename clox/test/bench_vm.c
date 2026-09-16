// bench_vm times the dispatch loop, which is the first thing the C side has
// ever had that could be timed -- chapter 14 ran nothing.
//
// Its immediate job is chapter 15's challenge 4: negating the top of the stack
// in place instead of push(-pop()). The same file is built twice, once with
// CLOX_NEGATE_VIA_STACK and once without, so the two are measured against each
// other rather than argued about. See `make bench`.
//
// Results go to stderr because the chunk ends in OP_RETURN, which prints what it
// pops -- tens of thousands of times. `make bench` sends stdout to /dev/null,
// which keeps that out of the measurement as well as out of the way.

#include <stdio.h>
#include <time.h>

#include "chunk.h"
#include "vm.h"

#ifdef CLOX_NEGATE_VIA_STACK
#define VARIANT "push(-pop())"
#else
#define VARIANT "negate in place"
#endif

static const int NEGATES = 1000;
static const int RUNS = 500000;
static const int REPS = 5;

static double timeChunk(Chunk* chunk, int runs) {
  clock_t start = clock();
  for (int i = 0; i < runs; i++) {
    interpret(chunk);
  }
  return (double)(clock() - start) / CLOCKS_PER_SEC;
}

int main(void) {
  initVM();
  vmSetTrace(NULL);

  // One constant, a thousand negates, a return. There are no jumps until chapter
  // 23, so the loop has to live out here rather than in the bytecode.
  Chunk chunk;
  initChunk(&chunk);
  writeConstant(&chunk, 1.0, 1);
  for (int i = 0; i < NEGATES; i++) {
    writeChunk(&chunk, OP_NEGATE, 1);
  }
  writeChunk(&chunk, OP_RETURN, 1);

  // Best of five, not the mean. A microbenchmark's slow runs are all noise --
  // a scheduler preemption, a frequency step, another process -- and none of
  // them make the code slower. The fastest run is the one with the least of
  // that in it.
  timeChunk(&chunk, RUNS / 20); // warm up
  double seconds = timeChunk(&chunk, RUNS);
  for (int i = 1; i < REPS; i++) {
    double next = timeChunk(&chunk, RUNS);
    if (next < seconds) seconds = next;
  }

  double total = (double)NEGATES * (double)RUNS;
  fprintf(stderr, "%-16s  %6.3f s  %5.2f ns per OP_NEGATE  (%.0f executed)\n",
          VARIANT, seconds, seconds * 1e9 / total, total);

  freeChunk(&chunk);
  freeVM();
  return 0;
}
