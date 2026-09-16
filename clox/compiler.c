#include <stdio.h>
#include <stdlib.h>

#include "common.h"
#include "compiler.h"
#include "debug.h"
#include "scanner.h"

// Parser holds the entire state of the parse, and there are two tokens in it.
//
// That is the headline of the chapter. jlox scans the whole file into a slice
// and the parser indexes into it, so it can look as far back and as far forward
// as it likes; the Go parser's `Current int` is exactly that index
// (parser/parser.go:13-18). Here there is no slice to index. previous is the
// token the current parse function is working on, current is the one token of
// lookahead, and anything older is gone.
//
// hadError and panicMode are two different questions. hadError is "did this
// compile" and is read once, at the end. panicMode is "are we still reeling
// from the last error" and exists only to stop one mistake from printing five
// messages.
typedef struct {
  Token current;
  Token previous;
  bool hadError;
  bool panicMode;
} Parser;

// Precedence is the binding power of an operator, as a number. That is the
// difference between this parser and both of the others in the repository: the
// Go recursive-descent front end encodes precedence in which function calls
// which (docs/front-end.md, "Precedence is the shape of the rule stack"), and
// the LL(k) one encodes it in which nonterminal derives which. Here it is data,
// and the table below is where it lives.
//
// PREC_CONDITIONAL is not the book's. It is where C puts ?: -- tighter than
// assignment, looser than `or` -- and challenge 3 needs a rung to hang the
// ternary on.
typedef enum {
  PREC_NONE,
  PREC_ASSIGNMENT,   // =
  PREC_CONDITIONAL,  // ?:
  PREC_OR,           // or
  PREC_AND,          // and
  PREC_EQUALITY,     // == !=
  PREC_COMPARISON,   // < > <= >=
  PREC_TERM,         // + -
  PREC_FACTOR,       // * /
  PREC_UNARY,        // ! -
  PREC_CALL,         // . ()
  PREC_PRIMARY,
} Precedence;

// The book writes this `void (*ParseFn)()`, an unprototyped pointer. Every other
// function in clox is declared (void), and clang has been warning about calls
// through unprototyped pointers since 15 -- it does not fire here yet, and this
// build is -Werror.
typedef void (*ParseFn)(void);

// One row per token type: what to do when the token starts an expression, what
// to do when it appears after one, and how tightly it binds in the second case.
// A NULL prefix means "this token cannot begin an expression", which is the only
// error the table itself can report.
typedef struct {
  ParseFn prefix;
  ParseFn infix;
  Precedence precedence;
} ParseRule;

Parser parser;

// The chunk being written to. It is a module global for the same reason the VM
// is: it saves threading a parameter through every emit function, at the cost of
// making two compilations at once impossible. Chapter 24 gives the compiler a
// struct and this becomes a field of it.
Chunk* compilingChunk;

// Where endCompiler's disassembly goes, or NULL for nowhere. See
// compilerSetTrace.
static FILE* codeTrace = NULL;

static Chunk* currentChunk(void) {
  return compilingChunk;
}

// ---------------------------------------------------------------------------
// Challenge 1: the parser, traced.
//
// The challenge asks for a written trace of which parse function calls which
// for `(-1 + 2) * 3 - -4`. Writing it by hand is the exercise. This is how the
// answer gets checked rather than believed -- `make parsetrace` builds clox with
// CLOX_TRACE_PARSER defined and runs it, and the chapter notes quote what came
// out. An ordinary build compiles none of it.
// ---------------------------------------------------------------------------
#ifdef CLOX_TRACE_PARSER
static int traceDepth = 0;

static const char* precedenceName(Precedence precedence) {
  switch (precedence) {
    case PREC_NONE:        return "NONE";
    case PREC_ASSIGNMENT:  return "ASSIGNMENT";
    case PREC_CONDITIONAL: return "CONDITIONAL";
    case PREC_OR:          return "OR";
    case PREC_AND:         return "AND";
    case PREC_EQUALITY:    return "EQUALITY";
    case PREC_COMPARISON:  return "COMPARISON";
    case PREC_TERM:        return "TERM";
    case PREC_FACTOR:      return "FACTOR";
    case PREC_UNARY:       return "UNARY";
    case PREC_CALL:        return "CALL";
    case PREC_PRIMARY:     return "PRIMARY";
  }
  return "?";
}

static void traceToken(const char* label, Token* token) {
  if (token->start == NULL) {
    fprintf(stderr, " %s=<none>", label);
  } else {
    fprintf(stderr, " %s='%.*s'", label, token->length, token->start);
  }
}

static void traceIn(const char* name, const char* detail) {
  fprintf(stderr, "%*s%s(%s)", traceDepth * 2, "", name,
          detail == NULL ? "" : detail);
  traceToken("previous", &parser.previous);
  traceToken("current", &parser.current);
  fprintf(stderr, "\n");
  traceDepth++;
}

static void traceOut(const char* name) {
  traceDepth--;
  fprintf(stderr, "%*s%s returns\n", traceDepth * 2, "", name);
}

#define TRACE_IN(name, detail) traceIn(name, detail)
#define TRACE_OUT(name) traceOut(name)
#else
#define TRACE_IN(name, detail) ((void)0)
#define TRACE_OUT(name) ((void)0)
#endif

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

// errorAt is where the cost of chapter 16's decision to put scanner errors in
// the token stream is paid, and it is one branch: a TOKEN_ERROR's start points
// at a message literal inside the scanner rather than into the source, so
// printing " at '%.*s'" for it would print the message twice.
//
// panicMode is set here and cleared only by a successful statement boundary --
// which this chapter does not have, so for now it is cleared once per
// compilation. Everything reported while it is set is dropped. The parser keeps
// parsing regardless: suppressing the *message* is not the same as stopping, and
// continuing is what finds the next real error.
static void errorAt(Token* token, const char* message) {
  if (parser.panicMode) return;
  parser.panicMode = true;

  fprintf(stderr, "[line %d] Error", token->line);

  if (token->type == TOKEN_EOF) {
    fprintf(stderr, " at end");
  } else if (token->type == TOKEN_ERROR) {
    // Nothing. The message is the token.
  } else {
    fprintf(stderr, " at '%.*s'", token->length, token->start);
  }

  fprintf(stderr, ": %s\n", message);
  parser.hadError = true;
}

// error reports at the token just consumed, errorAtCurrent at the one about to
// be. Which one a call site wants is a question about blame: "Expect ')'" is
// about where the ')' should have gone, so it points at what is there instead.
static void error(const char* message) {
  errorAt(&parser.previous, message);
}

static void errorAtCurrent(const char* message) {
  errorAt(&parser.current, message);
}

// ---------------------------------------------------------------------------
// The token stream
// ---------------------------------------------------------------------------

// advance is TOKEN_ERROR's first real consumer. The scanner never stops on a bad
// character -- it emits a token that *is* the complaint and carries on -- so the
// parser is what decides that a complaint is not an expression. Looping rather
// than returning means a run of junk characters costs one message, not one per
// character, because panicMode swallows the rest.
static void advance(void) {
  parser.previous = parser.current;

  for (;;) {
    parser.current = scanToken();
    if (parser.current.type != TOKEN_ERROR) break;

    errorAtCurrent(parser.current.start);
  }
}

static void consume(TokenType type, const char* message) {
  if (parser.current.type == type) {
    advance();
    return;
  }

  errorAtCurrent(message);
}

// ---------------------------------------------------------------------------
// Emitting
// ---------------------------------------------------------------------------

// Every byte is attributed to parser.previous, the token just consumed -- not to
// parser.current.
//
// The two differ less often than it looks, which is what makes this easy to get
// wrong and hard to notice. A binary operator's opcode is emitted after its
// whole right operand has been consumed, so previous is that operand's last
// token and current is whatever follows it -- usually on the same line. They
// part company when the next token starts a new one:
//
//     1 + 2
//     - 3
//
// The OP_ADD belongs on line 1 with the `+`, and parser.current is already the
// `-` on line 2. That source is in clox/test/test_compile.c for this reason.
static void emitByte(uint8_t byte) {
  writeChunk(currentChunk(), byte, parser.previous.line);
}

// The book has an emitBytes(byte1, byte2) here too, and exactly one caller:
// emitBytes(OP_CONSTANT, makeConstant(value)). writeConstant does that job
// instead, so there is nothing left for it to do and -Werror will not let it
// sit unused. It comes back in chapter 21, where every instruction with an
// operand is emitted by hand.
static void emitReturn(void) {
  emitByte(OP_RETURN);
}

// emitConstant goes through writeConstant rather than emitting OP_CONSTANT by
// hand, which is chapter 14's challenge 2 finally being paid for: the operand
// widens to three bytes past index 255 instead of the compiler refusing at 256.
//
// This is the first caller other than a hand-written demo, and the first time
// the compiler and the VM have to agree on the wide encoding with nobody holding
// both ends. The limit that remains -- 2^24 -- is what the book's "Too many
// constants in one chunk." error exists to report at 2^8, and it is not
// reachable by any program anyone will write.
static void emitConstant(Value value) {
  if (!writeConstant(currentChunk(), value, parser.previous.line)) {
    error("Too many constants in one chunk.");
  }
}

static void endCompiler(void) {
  emitReturn();

#ifdef DEBUG_PRINT_CODE
  // Only on success: a chunk that failed to compile has holes in it, and
  // disassembling it prints whichever bytes happened to land next to each other.
  if (!parser.hadError && codeTrace != NULL) {
    disassembleChunk(codeTrace, currentChunk(), "code");
  }
#endif
}

// ---------------------------------------------------------------------------
// The grammar
// ---------------------------------------------------------------------------

static void expression(void);
static void parsePrecedence(Precedence precedence);
static ParseRule* getRule(TokenType type);

// binary runs *after* the left operand has already been compiled and its
// bytecode emitted. That is the whole trick of the algorithm: by the time this
// is called, the left operand is on the stack at run time and behind us at
// compile time, so all that is left is the right operand and one opcode.
//
// precedence + 1 is what makes `-` left-associative. Parsing the right operand
// at one level tighter stops it from grabbing another `-`, so `1 - 2 - 3` comes
// out as `(1 - 2) - 3`. Passing `precedence` instead would make it right-
// associative and `1 - 2 - 3` would be 2. The Go parser says the same thing with
// control flow instead of a number -- a `for` loop for left, recursion for right
// (docs/front-end.md, "Associativity: loop or recurse").
static void binary(void) {
  TokenType operatorType = parser.previous.type;
  TRACE_IN("binary", tokenTypeName(operatorType));

  ParseRule* rule = getRule(operatorType);
  parsePrecedence((Precedence)(rule->precedence + 1));

  switch (operatorType) {
    case TOKEN_PLUS:  emitByte(OP_ADD); break;
    case TOKEN_MINUS: emitByte(OP_SUBTRACT); break;
    case TOKEN_STAR:  emitByte(OP_MULTIPLY); break;
    case TOKEN_SLASH: emitByte(OP_DIVIDE); break;
    default: break; // Unreachable.
  }

  TRACE_OUT("binary");
}

// grouping emits nothing at all. Parentheses exist to change what the parser
// builds, and once it has built it they have left no trace -- which is the same
// observation the Go RPN printer makes by dropping them (ast/rpn.go,
// VisitGroupingExpr). tool/rpndiff.sh is that coincidence turned into a test.
static void grouping(void) {
  TRACE_IN("grouping", NULL);
  expression();
  consume(TOKEN_RIGHT_PAREN, "Expect ')' after expression.");
  TRACE_OUT("grouping");
}

// number is the first time a clox token means a value rather than a span. The Go
// scanner did this during scanning and hung the result on the token as
// `Literal any`; here the text sat in the source buffer untouched until now.
static void number(void) {
  TRACE_IN("number", NULL);
  double value = strtod(parser.previous.start, NULL);
  emitConstant(value);
  TRACE_OUT("number");
}

// unary compiles the operand *before* it emits the operator, because the operand
// has to be on the stack before OP_NEGATE can pop it. Reading the two lines in
// that order is the first place bytecode stops looking like source.
//
// PREC_UNARY, not PREC_ASSIGNMENT: `-a.b + c` must negate `a.b` and then add,
// not negate the whole sum. Unary binds tighter than any binary operator, so the
// operand stops at the first thing that binds looser.
static void unary(void) {
  TokenType operatorType = parser.previous.type;
  TRACE_IN("unary", tokenTypeName(operatorType));

  parsePrecedence(PREC_UNARY);

  switch (operatorType) {
    case TOKEN_MINUS: emitByte(OP_NEGATE); break;
    default: break; // Unreachable.
  }

  TRACE_OUT("unary");
}

// ternary is challenge 3: `c ? a : b`, a mixfix operator, as one infix rule.
//
// The two precedences in here are the whole answer. The middle operand parses at
// the loosest precedence there is, because `?` and `:` bracket it exactly the
// way parentheses would -- `a ? b = 1 : c` is legal for the same reason
// `(b = 1)` is. The else branch parses at PREC_CONDITIONAL rather than one
// tighter, and that single missing `+ 1` is what makes `?:` right-associative:
// `a ? b : c ? d : e` groups as `a ? b : (c ? d : e)`, which is what C does and
// what every language that copied C does.
//
// No bytecode. Choosing between two operands means not evaluating one of them,
// which needs a jump, and jumps arrive in chapter 23. Emitting all three
// operands and hoping would compile and run and be wrong, so instead the
// operator parses correctly and then reports that it cannot be compiled -- which
// is checkable, and a comment is not.
static void ternary(void) {
  Token operatorToken = parser.previous;
  TRACE_IN("ternary", NULL);

  expression();
  consume(TOKEN_COLON, "Expect ':' after then branch of conditional.");
  parsePrecedence(PREC_CONDITIONAL);

  errorAt(&operatorToken,
          "The '?:' operator parses but cannot be compiled yet.");
  TRACE_OUT("ternary");
}

// The table. Reading a row: what this token does at the start of an expression,
// what it does in the middle of one, and how hard it pulls when it is in the
// middle.
//
// TOKEN_MINUS is the row worth staring at -- it is the only one in Lox with both
// columns filled, because `-` is negation in prefix position and subtraction in
// infix position, and the parser tells them apart by nothing more than where it
// was when it looked the token up. Challenge 2 is about that column.
//
// Everything still NULL here is a chapter that has not happened. Literals and
// identifiers are chapter 18 and 21, `and`/`or` are 23, `(` as a call and `.` as
// a property get are 24 and 27 -- and when those land, every one of them is a
// row in this table rather than a new function in a ladder.
ParseRule rules[] = {
  [TOKEN_LEFT_PAREN]    = {grouping, NULL,    PREC_NONE},
  [TOKEN_RIGHT_PAREN]   = {NULL,     NULL,    PREC_NONE},
  [TOKEN_LEFT_BRACE]    = {NULL,     NULL,    PREC_NONE},
  [TOKEN_RIGHT_BRACE]   = {NULL,     NULL,    PREC_NONE},
  [TOKEN_COMMA]         = {NULL,     NULL,    PREC_NONE},
  [TOKEN_DOT]           = {NULL,     NULL,    PREC_NONE},
  [TOKEN_MINUS]         = {unary,    binary,  PREC_TERM},
  [TOKEN_PLUS]          = {NULL,     binary,  PREC_TERM},
  [TOKEN_SEMICOLON]     = {NULL,     NULL,    PREC_NONE},
  [TOKEN_SLASH]         = {NULL,     binary,  PREC_FACTOR},
  [TOKEN_STAR]          = {NULL,     binary,  PREC_FACTOR},
  [TOKEN_QUESTION]      = {NULL,     ternary, PREC_CONDITIONAL},
  [TOKEN_COLON]         = {NULL,     NULL,    PREC_NONE},
  [TOKEN_BANG]          = {NULL,     NULL,    PREC_NONE},
  [TOKEN_BANG_EQUAL]    = {NULL,     NULL,    PREC_NONE},
  [TOKEN_EQUAL]         = {NULL,     NULL,    PREC_NONE},
  [TOKEN_EQUAL_EQUAL]   = {NULL,     NULL,    PREC_NONE},
  [TOKEN_GREATER]       = {NULL,     NULL,    PREC_NONE},
  [TOKEN_GREATER_EQUAL] = {NULL,     NULL,    PREC_NONE},
  [TOKEN_LESS]          = {NULL,     NULL,    PREC_NONE},
  [TOKEN_LESS_EQUAL]    = {NULL,     NULL,    PREC_NONE},
  [TOKEN_IDENTIFIER]    = {NULL,     NULL,    PREC_NONE},
  [TOKEN_STRING]        = {NULL,     NULL,    PREC_NONE},
  [TOKEN_NUMBER]        = {number,   NULL,    PREC_NONE},
  [TOKEN_INTERPOLATION] = {NULL,     NULL,    PREC_NONE},
  [TOKEN_AND]           = {NULL,     NULL,    PREC_NONE},
  [TOKEN_CLASS]         = {NULL,     NULL,    PREC_NONE},
  [TOKEN_ELSE]          = {NULL,     NULL,    PREC_NONE},
  [TOKEN_FALSE]         = {NULL,     NULL,    PREC_NONE},
  [TOKEN_FOR]           = {NULL,     NULL,    PREC_NONE},
  [TOKEN_FUN]           = {NULL,     NULL,    PREC_NONE},
  [TOKEN_IF]            = {NULL,     NULL,    PREC_NONE},
  [TOKEN_NIL]           = {NULL,     NULL,    PREC_NONE},
  [TOKEN_OR]            = {NULL,     NULL,    PREC_NONE},
  [TOKEN_PRINT]         = {NULL,     NULL,    PREC_NONE},
  [TOKEN_RETURN]        = {NULL,     NULL,    PREC_NONE},
  [TOKEN_SUPER]         = {NULL,     NULL,    PREC_NONE},
  [TOKEN_THIS]          = {NULL,     NULL,    PREC_NONE},
  [TOKEN_TRUE]          = {NULL,     NULL,    PREC_NONE},
  [TOKEN_VAR]           = {NULL,     NULL,    PREC_NONE},
  [TOKEN_WHILE]         = {NULL,     NULL,    PREC_NONE},
  [TOKEN_ERROR]         = {NULL,     NULL,    PREC_NONE},
  [TOKEN_EOF]           = {NULL,     NULL,    PREC_NONE},
};

// parsePrecedence is the entire algorithm, and it is fourteen lines.
//
// Read it as: consume one token and let it start an expression, then keep
// swallowing operators for as long as they bind at least as tightly as the
// caller asked for. The `precedence <= getRule(...)->precedence` test is the
// only comparison in the parser, and it replaces the nine nested function calls
// the Go front end spends on the same question -- `expression` down through
// `term` and `factor` to `unary`, one frame per rung, whether or not the
// expression uses any of them.
//
// A NULL prefix rule is the one error the table reports by itself: the token
// cannot begin an expression. That is `)` at the start, or `+`, or `else`.
static void parsePrecedence(Precedence precedence) {
  TRACE_IN("parsePrecedence", precedenceName(precedence));

  advance();
  ParseFn prefixRule = getRule(parser.previous.type)->prefix;
  if (prefixRule == NULL) {
    error("Expect expression.");
    TRACE_OUT("parsePrecedence");
    return;
  }

  prefixRule();

  while (precedence <= getRule(parser.current.type)->precedence) {
    advance();
    ParseFn infixRule = getRule(parser.previous.type)->infix;
    infixRule();
  }

  TRACE_OUT("parsePrecedence");
}

static ParseRule* getRule(TokenType type) {
  return &rules[type];
}

static void expression(void) {
  parsePrecedence(PREC_ASSIGNMENT);
}

// ---------------------------------------------------------------------------
// Entry points
// ---------------------------------------------------------------------------

bool compile(const char* source, Chunk* chunk) {
  initScanner(source);
  compilingChunk = chunk;

  // The parser is a global and the REPL reuses it, so it is reset rather than
  // assumed empty. previous in particular is read by error() before anything has
  // been consumed, if the very first token is bad.
  Token none = {TOKEN_EOF, NULL, 0, 0};
  parser.current = none;
  parser.previous = none;
  parser.hadError = false;
  parser.panicMode = false;

  advance();
  expression();
  consume(TOKEN_EOF, "Expect end of expression.");
  endCompiler();

  compilingChunk = NULL;
  return !parser.hadError;
}

// dumpTokens is chapter 16's compiler, kept. See compiler.h.
//
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

void dumpTokens(const char* source) {
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

void compilerSetTrace(FILE* out) {
  codeTrace = out;
}
