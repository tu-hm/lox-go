#ifndef clox_compiler_h
#define clox_compiler_h

#include <stdio.h>

#include "chunk.h"

// compile parses one expression and writes its bytecode into chunk. It returns
// false if the source did not compile, in which case chunk holds whatever was
// emitted before the error and the caller must not run it.
//
// One expression, not a program: the parser consumes an expression and then
// requires end of input. Statements arrive in chapter 21.
//
// Errors go to stderr as they are found, so a false return carries no detail --
// it has already been reported. That is the same contract the Go front end has
// through pkg/errors.HadError, reached by a different route: there the flag is a
// package global set as a side effect of building an error value, here it is a
// field of the parser that only this function reads.
bool compile(const char* source, Chunk* chunk);

// dumpTokens is what compile() was for one chapter: run the scanner to
// exhaustion and print every token. It survives as `clox -tokens` because it is
// the only view of the token stream there is, and because tool/scandiff.sh reads
// it to check the two scanners against each other.
void dumpTokens(const char* source);

// compilerSetTrace points the compiler's dump of the finished chunk at a stream,
// or switches it off with NULL. Same shape and same reasoning as vmSetTrace: the
// dump must never reach stdout, and without DEBUG_PRINT_CODE compiled in this
// records the stream and nothing reads it.
void compilerSetTrace(FILE* out);

#endif
