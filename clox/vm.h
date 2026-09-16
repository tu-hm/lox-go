#ifndef clox_vm_h
#define clox_vm_h

#include <stdio.h>

#include "chunk.h"
#include "value.h"

// VM is the whole machine: a chunk to run, a cursor into it, and a stack of
// temporaries.
//
// ip is a pointer into chunk->code rather than an index into it, which is the
// chapter's one unforced performance decision. An index would need the base
// address and the offset added on every fetch; a pointer is already the address.
// The cost is that ip is only valid while chunk is, so anything that reallocates
// the code array invalidates it -- see the same hazard on stackTop below.
typedef struct {
  Chunk* chunk;
  uint8_t* ip;

  // The book gives the stack a fixed 256 slots and does not check for overflow,
  // which makes the wrong bytecode undefined behaviour rather than an error.
  // Challenge 3 asks for a stack that grows instead, so this is a pointer and a
  // capacity, allocated through reallocate() like every other array here.
  //
  // stackTop points at the next free slot, not the top value -- so an empty
  // stack is stackTop == stack, and pushing needs no special case for it.
  Value* stack;
  Value* stackTop;
  int stackCapacity;

  // Where the execution trace goes, or NULL for no trace. See vmSetTrace.
  FILE* trace;
} VM;

typedef enum {
  INTERPRET_OK,
  INTERPRET_COMPILE_ERROR,
  INTERPRET_RUNTIME_ERROR,
} InterpretResult;

void initVM(void);
void freeVM(void);

// interpret runs a chunk from its first byte with an empty stack.
InterpretResult interpret(Chunk* chunk);

void push(Value value);
Value pop(void);

// vmSetTrace points the execution trace at a stream, or switches it off with
// NULL. It is a runtime switch on top of the compile-time DEBUG_TRACE_EXECUTION
// because the trace prints the entire stack before every instruction: on a chunk
// that pushes n values it is O(n^2) output, which drowns the thing being traced.
// Without DEBUG_TRACE_EXECUTION compiled in, this records the stream and nothing
// reads it.
void vmSetTrace(FILE* out);

// vmStackCapacity is how many slots the stack currently has room for. It exists
// so the demo can show challenge 3 having done something.
int vmStackCapacity(void);

#endif
