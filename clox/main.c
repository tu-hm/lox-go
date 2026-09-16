#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "chunk.h"
#include "common.h"
#include "compiler.h"
#include "debug.h"
#include "memory.h"
#include "vm.h"

#ifdef CLOX_OWN_HEAP
#include "heap.h"
#endif

// What to do with the source, once it has been read.
typedef enum {
  MODE_RUN,     // compile and execute -- the default
  MODE_TOKENS,  // scan only, and print the tokens
  MODE_DUMP,    // compile only, and print the bytecode
} Mode;

static Mode mode = MODE_RUN;

// run is the one place that decides what a chunk of source means, so the REPL
// and the file runner cannot drift apart. It returns the exit code the mode
// implies, which for everything but MODE_RUN is only ever 0 or 65.
static int run(const char* source) {
  switch (mode) {
    case MODE_TOKENS:
      dumpTokens(source);
      return 0;

    case MODE_DUMP: {
      // The disassembly is this mode's output, so it goes to stdout -- unlike
      // the one endCompiler writes, which is diagnostics and goes to stderr.
      // Same function, two streams; see debug.h.
      Chunk chunk;
      initChunk(&chunk);
      bool ok = compile(source, &chunk);
      if (ok) disassembleChunk(stdout, &chunk, "code");
      freeChunk(&chunk);
      return ok ? 0 : 65;
    }

    case MODE_RUN:
    default: {
      // 65 and 70 are the sysexits.h conventions the book uses and the numbers
      // tool/booktest.py already expects from the Go binary. Chapter 16 plumbed
      // them through with nothing behind them; the compiler is what makes 65
      // reachable.
      InterpretResult result = interpret(source);
      if (result == INTERPRET_COMPILE_ERROR) return 65;
      if (result == INTERPRET_RUNTIME_ERROR) return 70;
      return 0;
    }
  }
}

// repl reads a line and runs it. The 1024-byte limit is the book's and is a real
// limitation rather than a simplification: a longer line is silently split, and
// the halves compile as two separate programs. Fixing it needs a growable line
// buffer and changes nothing about the interpreter, which is why the book leaves
// it.
//
// An error does not end the session, which is the difference between this and
// runFile: the exit code is thrown away and the next line gets a fresh parser.
static void repl(void) {
  char line[1024];
  for (;;) {
    printf("> ");

    if (!fgets(line, sizeof(line), stdin)) {
      printf("\n");
      break;
    }

    run(line);
  }
}

// readFile slurps the whole file. It goes through reallocate rather than malloc
// -- a divergence from the book, and the point of the invariant memory.h states:
// reallocate is the only function in clox that allocates. Keeping the source
// buffer inside it is what lets `make test HEAP=own` still check at exit that
// nothing is live, and it is what chapter 26's byte counter will already be
// seeing without a special case.
static char* readFile(const char* path, size_t* length) {
  FILE* file = fopen(path, "rb");
  if (file == NULL) {
    fprintf(stderr, "Could not open file \"%s\".\n", path);
    exit(74);
  }

  fseek(file, 0L, SEEK_END);
  size_t fileSize = ftell(file);
  rewind(file);

  char* buffer = (char*)reallocate(NULL, 0, fileSize + 1);
  if (buffer == NULL) {
    fprintf(stderr, "Not enough memory to read \"%s\".\n", path);
    exit(74);
  }

  size_t bytesRead = fread(buffer, sizeof(char), fileSize, file);
  if (bytesRead < fileSize) {
    fprintf(stderr, "Could not read file \"%s\".\n", path);
    exit(74);
  }
  buffer[bytesRead] = '\0';

  fclose(file);
  *length = fileSize;
  return buffer;
}

static int runFile(const char* path) {
  size_t length;
  char* source = readFile(path, &length);
  int status = run(source);

  // The source is freed only after run returns, and that is load bearing: every
  // Token points into this buffer rather than owning a copy of its text, and the
  // compiler reads those pointers.
  FREE_ARRAY(char, source, length + 1);
  return status;
}

static void usage(void) {
  fprintf(stderr, "Usage: clox [-tokens|-dump] [-trace] [path]\n");
  exit(64);
}

// The flags mirror glox's (see main.go), because the two binaries are compared
// by tools that have to drive both: tool/scandiff.sh reads -tokens from each,
// and tool/rpndiff.sh reads -dump here against -print=rpn there.
int main(int argc, const char* argv[]) {
  bool trace = false;

  int i = 1;
  for (; i < argc && argv[i][0] == '-' && argv[i][1] != '\0'; i++) {
    if (strcmp(argv[i], "-tokens") == 0) {
      mode = MODE_TOKENS;
    } else if (strcmp(argv[i], "-dump") == 0) {
      mode = MODE_DUMP;
    } else if (strcmp(argv[i], "-trace") == 0) {
      trace = true;
    } else {
      usage();
    }
  }

  if (argc - i > 1) usage();

  initVM();

  // After initVM, not before: initVM sets vm.trace to NULL, so calling
  // vmSetTrace while parsing the flags above would be undone by it.
  //
  // Both traces go to stderr, so stdout stays exactly what it would have been.
  // In a release build DEBUG_TRACE_EXECUTION and DEBUG_PRINT_CODE are not
  // compiled in and this sets two variables nothing reads.
  if (trace) {
    vmSetTrace(stderr);
    compilerSetTrace(stderr);
  }

  int status = 0;
  if (i < argc) {
    status = runFile(argv[i]);
  } else {
    repl();
  }

  freeVM();

#ifdef CLOX_OWN_HEAP
  // Nothing may still be live at exit: the source buffer was freed, and so was
  // the VM's stack. LeakSanitizer would be the usual way to ask, and it is
  // unavailable on macOS. This writes to stderr so stdout stays byte for byte
  // identical to the libc build's.
  HeapStats stats = heapStats();
  if (stats.allocatedBlocks != 0 || !heapCheck()) {
    fprintf(stderr, "clox: %d block(s) still live at exit\n",
            stats.allocatedBlocks);
    return 1;
  }
#endif
  return status;
}
