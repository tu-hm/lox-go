#include <stdio.h>
#include <stdlib.h>

#include "common.h"
#include "memory.h"
#include "vm.h"

#ifdef CLOX_OWN_HEAP
#include "heap.h"
#endif

// repl reads a line and interprets it, which for this chapter means printing its
// tokens. The 1024-byte limit is the book's and is a real limitation rather than
// a simplification: a longer line is silently split, and the halves scan as two
// separate programs. Fixing it needs a growable line buffer and changes nothing
// about the interpreter, which is why the book leaves it.
static void repl(void) {
  char line[1024];
  for (;;) {
    printf("> ");

    if (!fgets(line, sizeof(line), stdin)) {
      printf("\n");
      break;
    }

    interpret(line);
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

static void runFile(const char* path) {
  size_t length;
  char* source = readFile(path, &length);
  InterpretResult result = interpret(source);

  // The source is freed only after interpret returns, and that is load bearing:
  // every Token points into this buffer rather than owning a copy of its text.
  FREE_ARRAY(char, source, length + 1);

  // 65 and 70 are the sysexits.h conventions the book uses and the numbers
  // tool/booktest.py already expects from the Go binary. Nothing can produce
  // either of them yet -- the compiler reports no errors and the VM runs no
  // bytecode -- but the plumbing is what chapter 17 fills in.
  if (result == INTERPRET_COMPILE_ERROR) exit(65);
  if (result == INTERPRET_RUNTIME_ERROR) exit(70);
}

int main(int argc, const char* argv[]) {
  initVM();

  if (argc == 1) {
    repl();
  } else if (argc == 2) {
    runFile(argv[1]);
  } else {
    fprintf(stderr, "Usage: clox [path]\n");
    exit(64);
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
  return 0;
}
