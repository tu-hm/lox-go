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

// DEBUG_PRINT_CODE compiles in the compiler's dump of what it just emitted: the
// finished chunk, disassembled, the moment the last byte is written.
//
// It follows DEBUG_TRACE_EXECUTION in both respects. It is tied to the
// Makefile's debug build rather than commented in and out by hand, and
// compiling it in is not the same as turning it on -- see compilerSetTrace.
// The dump goes to a stream the caller picks, and never to stdout, because
// stdout is the program's output and has to be identical in every build.
#ifdef DEBUG
#define DEBUG_PRINT_CODE
#endif

#endif
