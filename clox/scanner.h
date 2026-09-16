#ifndef clox_scanner_h
#define clox_scanner_h

typedef enum {
  // Single-character tokens.
  TOKEN_LEFT_PAREN, TOKEN_RIGHT_PAREN,
  TOKEN_LEFT_BRACE, TOKEN_RIGHT_BRACE,
  TOKEN_COMMA, TOKEN_DOT, TOKEN_MINUS, TOKEN_PLUS,
  TOKEN_SEMICOLON, TOKEN_SLASH, TOKEN_STAR,

  // Challenge 3 of chapter 17. Lox has no ternary, and neither scanner had a
  // token for either half of one until the compiler needed somewhere to hang
  // the mixfix parse rule. The Go scanner still has neither, which is the one
  // place the two token vocabularies differ -- see tool/scandiff.sh.
  TOKEN_QUESTION, TOKEN_COLON,

  // One or two character tokens.
  TOKEN_BANG, TOKEN_BANG_EQUAL,
  TOKEN_EQUAL, TOKEN_EQUAL_EQUAL,
  TOKEN_GREATER, TOKEN_GREATER_EQUAL,
  TOKEN_LESS, TOKEN_LESS_EQUAL,

  // Literals.
  TOKEN_IDENTIFIER, TOKEN_STRING, TOKEN_NUMBER,

  // Challenge 1: a string segment that ends at a "${" rather than at a quote,
  // so the tokens after it are an expression and the segment after *that* is
  // more of the same string.
  TOKEN_INTERPOLATION,

  // Keywords.
  TOKEN_AND, TOKEN_CLASS, TOKEN_ELSE, TOKEN_FALSE,
  TOKEN_FOR, TOKEN_FUN, TOKEN_IF, TOKEN_NIL, TOKEN_OR,
  TOKEN_PRINT, TOKEN_RETURN, TOKEN_SUPER, TOKEN_THIS,
  TOKEN_TRUE, TOKEN_VAR, TOKEN_WHILE,

  TOKEN_ERROR, TOKEN_EOF,
} TokenType;

// Token owns nothing. start points into the source buffer the caller passed to
// initScanner and length says how far it runs, so a token costs 24 bytes and no
// allocation -- where the Go implementation's token carries a `Lexeme string`
// and an already-parsed `Literal any`.
//
// Two consequences. The source buffer has to outlive every token taken from it,
// which is why runFile frees it only after interpret returns. And a number here
// is still text: nothing has called strtod, and nothing will until the compiler
// needs the value. The scanner's job is to say where the token is, not what it
// means.
//
// TOKEN_ERROR is the exception, and it is the interesting one. Its start points
// at a string literal in the scanner rather than into the source, so length is
// the message's length and not a span of the program. See errorToken.
typedef struct {
  TokenType type;
  const char* start;
  int length;
  int line;
} Token;

void initScanner(const char* source);

// scanToken produces the next token, and there is no list. The compiler asks for
// one when it wants one, which is the whole point of the chapter: jlox scans the
// entire file into a slice up front, and clox never holds more than two tokens
// at a time.
Token scanToken(void);

// tokenTypeName is the enum spelled without its TOKEN_ prefix -- LEFT_PAREN,
// AND, IDENTIFIER. The book's token dump prints the enum's integer instead,
// which is unreadable and, worse, renumbers every token below any one that gets
// inserted. These names are also exactly the ones the Go implementation's
// token.TokenType uses, which is what makes tool/scandiff.sh possible.
const char* tokenTypeName(TokenType type);

#endif
