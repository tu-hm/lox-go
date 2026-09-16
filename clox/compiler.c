#include <stdio.h>

#include "common.h"
#include "compiler.h"
#include "scanner.h"

// printLexeme writes a token's text with the whitespace escaped. The book prints
// it raw with %.*s, which is fine for eyeballing and wrong for a golden file: a
// multi-line string is one token and would span several lines of the dump, so
// nothing downstream could count tokens by counting lines.
static void printLexeme(const char* start, int length) {
  for (int i = 0; i < length; i++) {
    switch (start[i]) {
      case '\n': printf("\\n"); break;
      case '\r': printf("\\r"); break;
      case '\t': printf("\\t"); break;
      default: putchar(start[i]); break;
    }
  }
}

void compile(const char* source) {
  initScanner(source);

  // The bar-instead-of-a-repeated-line-number trick is the disassembler's, and
  // it is here for the same reason: a run of tokens from one source line should
  // read as a block. See disassembleInstruction in debug.c.
  int line = -1;
  for (;;) {
    Token token = scanToken();
    if (token.line != line) {
      printf("%4d ", token.line);
      line = token.line;
    } else {
      printf("   | ");
    }

    printf("%-14s '", tokenTypeName(token.type));
    printLexeme(token.start, token.length);
    printf("'\n");

    // The loop ends on TOKEN_EOF and not on TOKEN_ERROR, which is the scanner's
    // contract: an error is a token like any other, and scanning continues past
    // it. One bad character costs one token, not the rest of the file.
    if (token.type == TOKEN_EOF) break;
  }
}
