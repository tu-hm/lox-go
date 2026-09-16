#ifndef clox_common_h
#define clox_common_h

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

// DEBUG_TRACE_EXECUTION compiles in the VM's execution trace: every instruction
// disassembled as it runs, with the stack printed beside it.
//
// The book defines this unconditionally and tells you to comment it out. Here it
// follows the Makefile's own debug/release split instead, so a release build
// carries none of it -- the trace is a branch and a formatted write per
// instruction, which is the most expensive thing in the dispatch loop by a wide
// margin.
//
// Compiling it in is not the same as turning it on. See vmSetTrace.
#ifdef DEBUG
#define DEBUG_TRACE_EXECUTION
#endif

#endif
