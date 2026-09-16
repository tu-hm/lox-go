#ifndef clox_compiler_h
#define clox_compiler_h

// compile turns source text into... nothing, for one chapter. Chapter 16's
// compiler exists to give the scanner a consumer and to prove the token stream
// is right; it prints the tokens and returns. Chapter 17 is where it starts
// emitting bytecode, and where this signature grows a Chunk* and a bool.
void compile(const char* source);

#endif
