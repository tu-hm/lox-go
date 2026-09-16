#include <stdio.h>

#include "common.h"
#include "debug.h"
#include "memory.h"
#include "vm.h"

// One global VM, as the book has it. The argument for it is that passing a VM*
// through every function costs a register everywhere for a flexibility nothing
// uses; the argument against is that it makes two interpreters in one process
// impossible. Chapter 15 is not where that trade is settled.
VM vm;

// The stack starts at the book's fixed size, so any program that would have fit
// in the book's VM never reallocates at all and the growth path below is dead
// code for it. Past that it doubles, like every other array here.
#define STACK_INITIAL 256

static void resetStack(void) {
  vm.stackTop = vm.stack;
}

static void growStack(void) {
  // The trap in challenge 3, in three lines. stackTop is a pointer *into* the
  // stack, so a reallocation that moves the buffer leaves it dangling -- and it
  // dangles silently, because the old address is still readable memory for a
  // while. Save the height, grow, then rebuild the pointer from the new base.
  //
  // Nothing else in the VM holds a pointer into the stack today. Chapter 24
  // gives every call frame a `slots` pointer into it, and this function is where
  // that will have to be dealt with.
  int height = vm.stack == NULL ? 0 : (int)(vm.stackTop - vm.stack);

  int oldCapacity = vm.stackCapacity;
  vm.stackCapacity = oldCapacity < STACK_INITIAL ? STACK_INITIAL
                                                 : GROW_CAPACITY(oldCapacity);
  vm.stack = GROW_ARRAY(Value, vm.stack, oldCapacity, vm.stackCapacity);
  vm.stackTop = vm.stack + height;
}

void initVM(void) {
  vm.chunk = NULL;
  vm.ip = NULL;
  vm.stack = NULL;
  vm.stackTop = NULL;
  vm.stackCapacity = 0;
  vm.trace = NULL;

  growStack();
  resetStack();
}

void freeVM(void) {
  FREE_ARRAY(Value, vm.stack, vm.stackCapacity);
  vm.stack = NULL;
  vm.stackTop = NULL;
  vm.stackCapacity = 0;
}

void vmSetTrace(FILE* out) {
  vm.trace = out;
}

int vmStackCapacity(void) {
  return vm.stackCapacity;
}

void push(Value value) {
  if (vm.stackTop - vm.stack == vm.stackCapacity) growStack();
  *vm.stackTop = value;
  vm.stackTop++;
}

Value pop(void) {
  vm.stackTop--;
  return *vm.stackTop;
}

static InterpretResult run(void) {
#define READ_BYTE() (*vm.ip++)
#define READ_CONSTANT() (vm.chunk->constants.values[READ_BYTE()])

// BINARY_OP is a macro rather than a function because it is parameterised by an
// operator, which C has no other way to pass. The do/while(false) wrapper is
// what lets `BINARY_OP(+);` behave like one statement: without it, the expansion
// is a block, and `if (x) BINARY_OP(+); else ...` stops compiling.
//
// b pops first. The compiler pushes the left operand first, so the right one is
// on top -- popping in source order would evaluate 3 - 2 as 2 - 3, which is
// invisible for + and * and wrong for - and /.
#define BINARY_OP(op) \
    do { \
      double b = pop(); \
      double a = pop(); \
      push(a op b); \
    } while (false)

  for (;;) {
#ifdef DEBUG_TRACE_EXECUTION
    if (vm.trace != NULL) {
      fprintf(vm.trace, "          ");
      for (Value* slot = vm.stack; slot < vm.stackTop; slot++) {
        fprintf(vm.trace, "[ ");
        fprintValue(vm.trace, *slot);
        fprintf(vm.trace, " ]");
      }
      fprintf(vm.trace, "\n");
      disassembleInstruction(vm.trace, vm.chunk,
                             (int)(vm.ip - vm.chunk->code));
    }
#endif

    uint8_t instruction;
    switch (instruction = READ_BYTE()) {
      case OP_CONSTANT: {
        Value constant = READ_CONSTANT();
        push(constant);
        break;
      }
      case OP_CONSTANT_LONG: {
        // Three reads in three statements, deliberately. Written as one
        // expression -- READ_BYTE() | (READ_BYTE() << 8) | (READ_BYTE() << 16)
        // -- this compiles, runs, and is wrong: C does not specify the order in
        // which the operands of | are evaluated, so the three bytes can come
        // back permuted, and whether they do is a property of the optimiser.
        //
        // This is the first code other than the disassembler to decode chapter
        // 14's 24-bit operand. The two decoders must agree; the golden test is
        // what says they do.
        int constant = READ_BYTE();
        constant |= READ_BYTE() << 8;
        constant |= READ_BYTE() << 16;
        push(vm.chunk->constants.values[constant]);
        break;
      }
      case OP_ADD:      BINARY_OP(+); break;
      case OP_SUBTRACT: BINARY_OP(-); break;
      case OP_MULTIPLY: BINARY_OP(*); break;
      case OP_DIVIDE:   BINARY_OP(/); break;
      case OP_NEGATE:
        // Challenge 4. The book writes push(-pop()), which decrements stackTop
        // and then increments it back for a value that never moves. Negating in
        // place leaves stackTop alone.
        //
        // The book's version stays here behind a flag rather than in a comment,
        // because the challenge ends with "see if you can measure a performance
        // difference" and that is not a question to answer by reading. Both are
        // built and timed by `make bench`; the chapter notes have the numbers.
#ifdef CLOX_NEGATE_VIA_STACK
        push(-pop());
#else
        vm.stackTop[-1] = -vm.stackTop[-1];
#endif
        break;
      case OP_RETURN: {
        printValue(pop());
        printf("\n");
        return INTERPRET_OK;
      }
      default:
        // Unreachable from any chunk this program builds, and reachable from a
        // corrupt one. The disassembler's answer to an unknown byte is to report
        // it and step over; the VM cannot, because it has no idea how wide the
        // instruction was.
        fprintf(stderr, "clox: unknown opcode %d\n", instruction);
        return INTERPRET_RUNTIME_ERROR;
    }
  }

#undef BINARY_OP
#undef READ_CONSTANT
#undef READ_BYTE
}

InterpretResult interpret(Chunk* chunk) {
  vm.chunk = chunk;
  vm.ip = vm.chunk->code;

  // The book does not reset here, because its main() runs exactly one chunk.
  // The demo runs four, and a chunk that ends with values still on the stack --
  // OP_RETURN pops one, not all of them -- would otherwise leak them into the
  // next one.
  resetStack();

  return run();
}
